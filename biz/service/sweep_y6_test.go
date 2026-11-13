package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/yi-nology/open-voice-bridge/internal/appconfig"
	"github.com/yi-nology/open-voice-bridge/internal/config"
)

// TestASRConfigReturnsSnapshot pins issue #11: ASRConfig must hand out a copy
// of the provider table, never the live map. A caller writing into (or simply
// iterating) the returned map used to rewrite the running configuration and
// race the next save's writers into a process-fatal concurrent map access.
func TestASRConfigReturnsSnapshot(t *testing.T) {
	svc := testService(t)
	summary := svc.ASRConfig(context.Background())
	providers, ok := summary["providers"].(map[string]config.ASRProviderConfig)
	if !ok {
		t.Fatalf("providers = %T, want the typed provider table", summary["providers"])
	}
	provider, ok := providers["mimo"]
	if !ok {
		t.Fatal("default provider missing from ASRConfig")
	}
	provider.Args = append(provider.Args, "caller-injected")
	provider.Model = "caller-mutated"
	providers["mimo"] = provider
	delete(providers, "mimo")

	svc.cfgMu.RLock()
	live, alive := svc.cfg.ASR.Providers["mimo"]
	svc.cfgMu.RUnlock()
	if !alive {
		t.Fatal("ASRConfig handed out the live internal provider map")
	}
	if live.Model == "caller-mutated" {
		t.Fatal("a caller mutation reached the running configuration")
	}
	for _, arg := range live.Args {
		if arg == "caller-injected" {
			t.Fatal("a caller mutation reached the shared Args backing array")
		}
	}
}

// TestConfigSaveDoesNotMutateCaller pins the write side of issue #11: Save
// takes the config by value, but the Providers map travels by reference. The
// normaliser must build its adjusted table on a private map instead of
// rewriting the caller's running state from inside the save path.
func TestConfigSaveDoesNotMutateCaller(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cfg.ASRModel = "probe-model"
	mimo := cfg.ASR.Providers["mimo"]
	mimo.Model = "" // normalisation fills it — on the caller's map, before the fix
	cfg.ASR.Providers["mimo"] = mimo

	if err := appconfig.Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := cfg.ASR.Providers["mimo"].Model; got != "" {
		t.Fatalf("Save rewrote the caller's provider map (model=%q)", got)
	}
	// Normalisation still happens — on the persisted document.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "probe-model") {
		t.Fatal("the persisted document lost the mimo default model")
	}
}

// TestConfigSaveDoesNotHoldLockDuringDiskWrite pins issue #13 (R8/I3/CON-08):
// a config save must not extend the cfgMu window across its fsync. The FIFO
// with no reader parks the write inside the file open, and pure-memory
// readers must stay responsive throughout.
func TestConfigSaveDoesNotHoldLockDuringDiskWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fifo")
	}
	svc := testService(t)
	dir := t.TempDir()
	svc.path = filepath.Join(dir, "config.json")
	fifo := svc.path + ".tmp"
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := svc.Apply(context.Background(), map[string]string{"up": "esc"})
		done <- err
	}()

	// Give Apply a moment to reach the FIFO open, then probe the read side.
	time.Sleep(100 * time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := make(chan struct{}, 1)
		go func() { svc.TextConfig(context.Background()); got <- struct{}{} }()
		select {
		case <-got:
		case <-time.After(700 * time.Millisecond):
			t.Fatal("a config read blocked behind the in-flight disk write: the save runs under cfgMu")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Unblock the writer; fsync on a pipe is EINVAL, and the lock discipline —
	// what this test guards — is already proved above.
	reader, err := os.Open(fifo)
	if err != nil {
		t.Fatalf("open fifo: %v", err)
	}
	defer func() { _ = reader.Close() }()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, syscall.EINVAL) {
			t.Fatalf("apply: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("apply never finished")
	}
}

// TestASRConfigReadAndSaveRace pins the interleaving that used to be a
// process-fatal "concurrent map read and map write": one goroutine reading the
// ASR config (and iterating the returned table) while another saves configs.
// Run with -race; the fixed shape (snapshot out, snapshot save) must survive.
func TestASRConfigReadAndSaveRace(t *testing.T) {
	svc := testService(t)
	var wg sync.WaitGroup
	wg.Add(2)
	stop := make(chan struct{})
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			select {
			case <-stop:
				return
			default:
			}
			summary := svc.ASRConfig(context.Background())
			providers, _ := summary["providers"].(map[string]config.ASRProviderConfig)
			for _, provider := range providers {
				_ = provider.Model
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 60; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = svc.Apply(context.Background(), map[string]string{"up": "esc"})
		}
		close(stop)
	}()
	wg.Wait()
}
