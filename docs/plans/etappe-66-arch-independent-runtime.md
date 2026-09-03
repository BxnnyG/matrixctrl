# Etappe 66 — Removing the one thing that needed emulation

P2-7: releases are `linux/amd64` only. Two attempts at multi-arch failed in the image
step; the entry's suspicion was the runtime stage's `apk add` running under QEMU, and a
local reproduction died in exactly that spot.

## The diagnosis holds, and it is narrower than it looks

The Dockerfile is already built for cross-compilation:

```dockerfile
FROM --platform=$BUILDPLATFORM node:20-alpine AS frontend   # native
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS backend # native
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build …  # cross-compiles

FROM alpine:3.21                       # ← the target architecture
RUN apk add --no-cache ca-certificates tzdata   # ← the only emulated command
```

Every stage that does work runs natively. The binary is cross-compiled by Go. **One
command in the whole build needs the target architecture to execute**, and it is a
package install that fetches two files.

So the fix is not "make emulation work". It is to stop needing it.

## What those two packages are actually for

- **`tzdata`** — Go's standard library can carry the timezone database itself, with a
  single blank import. Nothing has to be installed at all.
- **`ca-certificates`** — a bundle of PEM text. It is architecture-independent by
  nature, so it can be copied out of the builder stage, which already has one and
  already runs natively.

After that the runtime stage runs no commands: it copies a cross-compiled binary and a
text file. Nothing in it depends on the target architecture executing, which is what
made the earlier attempts fail.

## Honest limits of what is verified here

This host has **no QEMU** — `docker buildx inspect` offers `linux/amd64` only — so the
arm64 image cannot be built or tested from here at all. What can be established locally
is that the amd64 image still works without the packages: TLS to an external host
(release notes are fetched over HTTPS) and a timezone lookup.

Whether arm64 then builds is CI's answer, at the next tag. That is stated rather than
implied: this etappe removes the known cause, it does not prove the cure.

## Scope

**Ships:** the emulation-free runtime stage, the workflow building both architectures on
native runners, and a README that stops being silent about the current limitation.

**Does not ship: `FROM scratch`.** Alpine without `apk add` is already inert, and a
shell in the image is worth keeping for the moment somebody has to look inside a pod
that will not start.

## Definition of done

- The runtime stage runs no command
- The amd64 image still verifies TLS and resolves a timezone, checked on the built image
- `linux/arm64` is requested by the release workflow, on a native runner
- The README says what architectures a release actually carries
- `make check` green, and the deployed panel keeps working
