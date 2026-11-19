import React from "react";
import {
  Easing,
  interpolate,
  Interactive,
  useCurrentFrame,
  useVideoConfig,
} from "remotion";
import { SceneShell, SubtitleBar } from "../components/shell";
import { COLORS } from "../theme";
import { MicGlyph } from "./Scene1Hook";

const NODES = [
  { label: "遥控器", sub: "按住语音键" },
  { label: "BLE / ATVV", sub: "16 kHz 音频" },
  { label: "ASR 识别", sub: "MiMo 等多引擎" },
  { label: "焦点窗口", sub: "直接上屏" },
];

export const Scene3Flow: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();

  // pulse travels the pipeline twice
  const cycle = 4.6 * fps;
  const local = (frame - 0.6 * fps) % cycle;
  const progress = interpolate(local, [0, cycle * 0.72], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.bezier(0.45, 0, 0.2, 1),
  });
  const pulseVisible = frame > 0.6 * fps && local < cycle * 0.8;

  return (
    <SceneShell glowX="50%" glowY="34%">
      <Interactive.Div
        name="Flow title"
        style={{
          position: "absolute",
          top: 120,
          width: "100%",
          textAlign: "center",
          fontSize: 64,
          fontWeight: 700,
          letterSpacing: 3,
          opacity: interpolate(frame, [0, 0.5 * fps], [0, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            easing: Easing.bezier(0.16, 1, 0.3, 1),
          }),
        }}
      >
        说话，就到了<span style={{ color: COLORS.accent }}>光标所在的地方</span>
      </Interactive.Div>

      <div
        style={{
          position: "absolute",
          top: 330,
          left: 140,
          right: 140,
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        {NODES.map((n, i) => {
          const threshold = i / (NODES.length - 1);
          const lit = progress >= threshold - 0.02;
          return (
            <React.Fragment key={n.label}>
              <Interactive.Div
                name={`Node ${n.label}`}
                style={{
                  width: 320,
                  height: 180,
                  borderRadius: 22,
                  background: lit
                    ? "rgba(45,212,191,0.12)"
                    : "rgba(255,255,255,0.04)",
                  border: `1.5px solid ${lit ? "rgba(45,212,191,0.7)" : "rgba(255,255,255,0.12)"}`,
                  boxShadow: lit ? "0 0 46px rgba(45,212,191,0.28)" : "none",
                  display: "flex",
                  flexDirection: "column",
                  alignItems: "center",
                  justifyContent: "center",
                  gap: 12,
                }}
              >
                {i === 0 ? (
                  <div
                    style={{
                      width: 64,
                      height: 64,
                      borderRadius: "50%",
                      background: "rgba(45,212,191,0.2)",
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "center",
                    }}
                  >
                    <MicGlyph size={32} color={COLORS.accent} />
                  </div>
                ) : null}
                <div style={{ fontSize: 38, fontWeight: 700, letterSpacing: 1 }}>{n.label}</div>
                <div style={{ fontSize: 26, color: COLORS.muted, letterSpacing: 1 }}>{n.sub}</div>
              </Interactive.Div>
              {i < NODES.length - 1 ? (
                <div
                  style={{
                    flex: 1,
                    height: 3,
                    margin: "0 10px",
                    background:
                      "linear-gradient(90deg, rgba(45,212,191,0.7), rgba(76,125,255,0.55))",
                    borderRadius: 999,
                    position: "relative",
                    opacity: 0.85,
                  }}
                >
                  <div
                    style={{
                      position: "absolute",
                      right: -2,
                      top: -9,
                      width: 0,
                      height: 0,
                      borderLeft: "14px solid rgba(45,212,191,0.8)",
                      borderTop: "10px solid transparent",
                      borderBottom: "10px solid transparent",
                    }}
                  />
                </div>
              ) : null}
            </React.Fragment>
          );
        })}
      </div>

      {/* traveling pulse + start ripple */}
      <div
        style={{
          position: "absolute",
          top: 412,
          left: 300,
          width: 1320,
          height: 16,
          opacity: pulseVisible ? 1 : 0,
        }}
      >
        <div
          style={{
            position: "absolute",
            left: `${progress * 100}%`,
            top: 0,
            width: 18,
            height: 18,
            marginLeft: -9,
            borderRadius: "50%",
            background: COLORS.accent,
            boxShadow: `0 0 24px 8px rgba(45,212,191,0.65)`,
          }}
        />
      </div>

      <LyricCard />
      <SubtitleBar text="按住语音键说话，识别结果直接输入当前焦点窗口，同时以歌词样式显示在字幕屏。不用切换窗口，也不用先打开页面。" />
    </SceneShell>
  );
};

const LyricCard: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const text = "今天天气真不错，我们出去走走吧";
  const chars = text.split("");
  const start = 1.2 * fps;
  return (
    <Interactive.Div
      name="Lyric glass panel"
      style={{
        position: "absolute",
        left: "50%",
        top: 620,
        translate: "-50% 0px",
        width: 1080,
        padding: "34px 48px",
        borderRadius: 26,
        background: "rgba(18,26,43,0.78)",
        border: "1px solid rgba(255,255,255,0.1)",
        backdropFilter: "blur(22px)",
        opacity: interpolate(frame, [0.7 * fps, 1.3 * fps], [0, 1], {
          extrapolateLeft: "clamp",
          extrapolateRight: "clamp",
          easing: Easing.bezier(0.16, 1, 0.3, 1),
        }),
      }}
    >
      <div style={{ fontSize: 24, color: COLORS.muted, letterSpacing: 3, marginBottom: 18 }}>
        字幕屏 · LYRICS
      </div>
      <div style={{ fontSize: 46, fontWeight: 600, letterSpacing: 2, lineHeight: 1.5 }}>
        {chars.map((c, i) => {
          const on = frame > start + i * 2.2;
          return (
            <span key={i} style={{ color: on ? COLORS.accent : "rgba(242,246,255,0.22)" }}>
              {c}
            </span>
          );
        })}
      </div>
    </Interactive.Div>
  );
};
