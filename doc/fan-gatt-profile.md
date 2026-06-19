# Wind-Simulator Fan — BLE GATT Profile

> **Status:** contract / reference copy. The source of truth for this profile
> lives with the firmware. This copy is committed alongside the controlling
> application (`simtelizo-dev`) so the app and firmware stay in sync; lift it
> into the firmware repository (e.g. `docs/fan-gatt-profile.md` there) when
> implementing the firmware side. It is fully self-contained — implementing the
> firmware requires nothing from the Go application.

This document is the contract between the wind-simulator fan firmware and the
controlling application. The firmware exposes a single custom GATT service with
typed characteristics. There is **no** UART / Nordic UART Service emulation —
the previous `FAN:`/`STATUS:` ASCII-over-NUS protocol is replaced entirely.

## Service & characteristics

All UUIDs share one 128-bit base. The values below are proposed concrete UUIDs;
they may be regenerated before release, but if changed they MUST be updated in
both this document and the application's `fancontroller` package. All multi-byte
integers are **little-endian**.

| Name | UUID | Properties | Payload |
|---|---|---|---|
| Fan Service | `7a3e0001-87d1-3091-411d-000002373705` | primary service | — |
| Fan Duty | `7a3e0002-87d1-3091-411d-000002373705` | Write / Write Without Response (encrypted) | `u8` duty, 0–100 |
| Fan Status | `7a3e0003-87d1-3091-411d-000002373705` | Read, Notify | `u8` duty (0–100), `u16` rpm — 3 bytes |
| Fan Capabilities | `7a3e0004-87d1-3091-411d-000002373705` | Read | `u16` protocolVersion, `u16` flags — 4 bytes |
| Fan Control | `7a3e0005-87d1-3091-411d-000002373705` | Write / Write Without Response (encrypted) | `u8` opcode |
| Fan Event | `7a3e0006-87d1-3091-411d-000002373705` | Notify | `u8` eventId — 1 byte |
| Display Text | `7a3e0007-87d1-3091-411d-000002373705` | Write / Write Without Response (encrypted) | UTF-8 string, ≤64 bytes |

### Fan Duty (write)
- Single byte, 0–100 inclusive. Values >100 MUST be clamped to 100.
- Write Without Response (no ATT acknowledgement); latest write wins.
- Requires an **encrypted (bonded) link**: writes on an unencrypted link MUST
  be rejected with ATT *Insufficient Authentication* (`0x05`). The central must
  be bonded before writing (see Connection & lifecycle).
- Firmware applies the value to the PWM output immediately.

### Fan Status (read + notify)
- Layout: byte 0 = current duty (0–100); bytes 1–2 = measured RPM as `u16` LE.
- Readable at any time; also pushed via notification **on change** of duty or a
  significant RPM delta (and once on connect to seed initial state). There is no
  application-level heartbeat: the central tracks liveness via the BLE link
  supervision timeout, not via periodic status pushes.
- RPM of an unknown/stalled fan reports 0.

### Fan Capabilities (read)
- Layout: bytes 0–1 = `protocolVersion` (`u16` LE); bytes 2–3 = `flags` (`u16` LE).
- `protocolVersion` starts at `1`. Increment on any breaking layout/semantics
  change. The app reads this on connect and refuses devices it doesn't support.
- `flags` is a reserved capability bitfield (all zero for v1).

### Fan Control (write)
- Single-byte opcode. Requires an **encrypted (bonded) link** (same as Fan
  Duty); writes on an unencrypted link MUST be rejected with ATT *Insufficient
  Authentication* (`0x05`). Defined opcodes:
  - `0x01` — UNPAIR: remove bonding / pairing state and disconnect.
- Unknown opcodes MUST be ignored.

## Advertising

- The advertisement (or scan response) MUST include the **Fan Service** UUID
  (`7a3e0001-87d1-3091-411d-000002373705`) so the controller can recognise the
  device as a fan before connecting. Optionally include a short local name
  (e.g. `WindSim`).
- No NUS UUID is advertised.

## Connection & lifecycle

- The device acts as a BLE peripheral; the app is the central and initiates
  connection by address after pairing.
- Pairing: `NoInputNoOutput` (Just Works) with LE Secure Connections and
  bonding. The Fan Duty and Fan Control characteristics require encryption, so
  the central MUST bond before writing them; Status and Capabilities
  reads/notifications are available on an unencrypted link.
- On disconnect, the firmware MUST drive the fan to a safe state (duty 0) unless
  product requirements state otherwise.

## Versioning

- Additive characteristics/flags do not require a `protocolVersion` bump.
- Any change to an existing characteristic's byte layout or semantics MUST bump
  `protocolVersion`.

## Migration note

During rollout the controller may also recognise legacy NUS-based firmware
(service `6e400001-b5a3-f393-e0a9-e50e24dcca9e`). New firmware MUST NOT depend on
that path; it exists only so un-reflashed devices keep working until retired.
