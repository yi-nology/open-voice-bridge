#ifndef BLE_BRIDGE_DARWIN_H
#define BLE_BRIDGE_DARWIN_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/* Callbacks run on the bridge's private serial dispatch queue. Keep them
   short: copy what you need and hand off; never block. No callback fires
   after atvv_close returns. */
typedef void (*atvv_data_cb)(const uint8_t *data, size_t len, void *user);
typedef void (*atvv_event_cb)(int event, const char *message, void *user);

enum {
	ATVV_EVENT_DISCONNECTED = 1,
	ATVV_EVENT_GATT_READY = 2,
	ATVV_EVENT_CONNECT_FAILED = 3,
	ATVV_EVENT_SUBSCRIBED = 4,
	ATVV_EVENT_SUBSCRIBE_FAILED = 5
};

/* Allocate a session. id is a CoreBluetooth peripheral identifier (UUID
   string) or empty for auto-resolution: already-connected peripherals
   exposing the ATVV or HID service first, advertising scan as fallback.
   name_hint (may be empty) filters candidates by GAP name substring.
   Non-blocking; returns NULL only on allocation failure. */
void *atvv_open(const char *id, const char *name_hint,
	atvv_data_cb on_audio, atvv_data_cb on_control, atvv_event_cb on_event,
	void *user, char *errbuf, size_t errlen);

/* Kick off resolve + connect + ATVV service/characteristic discovery.
   Returns 0 when the pipeline started; completion arrives as
   ATVV_EVENT_GATT_READY or ATVV_EVENT_CONNECT_FAILED. */
int atvv_start_connect(void *handle, char *errbuf, size_t errlen);

/* Enable notifications on the audio and control characteristics.
   Returns 0 when started; completion arrives as ATVV_EVENT_SUBSCRIBED or
   ATVV_EVENT_SUBSCRIBE_FAILED. Requires GATT_READY. */
int atvv_start_subscribe(void *handle, char *errbuf, size_t errlen);

/* Write to the TX characteristic: write-without-response when the
   characteristic supports it (ATVV's normal mode), write-with-response
   otherwise. Returns 0 when the write was issued. */
int atvv_write(void *handle, const uint8_t *data, size_t len,
	char *errbuf, size_t errlen);

/* Stable CoreBluetooth identifier of the connected peripheral. Valid until
   atvv_close; do not free. Returns NULL before GATT_READY. */
const char *atvv_identifier(void *handle);

/* Tear down and unblock anything in flight. Idempotent. After it returns no
   further callbacks fire, so the caller may release `user`. */
void atvv_close(void *handle);

#ifdef __cplusplus
}
#endif

#endif /* BLE_BRIDGE_DARWIN_H */
