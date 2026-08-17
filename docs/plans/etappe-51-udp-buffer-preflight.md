# Etappe 51 — The buffer the SFU asked for and did not get

P2-24. LiveKit warns on every start that its UDP receive buffer is far below what a
production setup needs. The panel has never said so, and the number is knowable from
inside — which is exactly the scope the backlog entry gave it.

## Reproduced first

```
$ kubectl -n ess logs <sfu> | grep "receive buffer"
WARN livekit rtcconfig/rtc_unix.go:31 UDP receive buffer is too small for a
     production set-up  {"current": 425984, "suggested": 5000000}
```

Still true on the running SFU. And the drop counter, from the SFU's own metrics:

```
livekit_node_packet_total{node_id="…",node_type="SERVER",type="dropped"} 0
```

So the fault is **latent**: the buffer is undersized, nothing has been dropped yet.
The panel has to say both halves, because "your buffer is too small" on its own reads
as an active fault and sends someone chasing a problem that is not happening.

## The trap this etappe exists to avoid

The obvious implementation reads `/proc/sys/net/core/rmem_max` and
`/proc/net/snmp` from MatrixCtrl's own process. Both are network-namespaced, and
**MatrixCtrl does not run with `hostNetwork`** while the SFU does. Measured:

| | InDatagrams | RcvbufErrors |
|---|---|---|
| node | 48009 | 0 |
| matrixctrl pod | **320** | 0 |

A drop counter read from MatrixCtrl's pod reports MatrixCtrl's own UDP traffic. It
would sit at zero forever no matter what the SFU experiences, and it would look
authoritative doing it — `addrs[0]` and §4.43 again, a value that is a member of the
wrong set.

So **both numbers come from the SFU's own vantage point**: the sizes from LiveKit's
startup warning, the drops from `livekit_node_packet_total{type="dropped"}`, which is
already on the metrics endpoint this project scrapes and is already in the package's
test fixture — parsed for `type="out"` and never for `type="dropped"`.

## A smaller trap: which "current" is current

`net.core.rmem_max` is **212992** on this node; LiveKit reports `current: 425984`,
exactly double. That is Linux's `SO_RCVBUF` accounting — the kernel returns twice what
was set. Both numbers are defensible and they are not the same number, so the panel
reports **LiveKit's**, which is what the operator will see in the SFU's own log. A
panel that contradicts the component's log costs more time than it saves. (The
backlog's "24× smaller" came from the sysctl value; against LiveKit's own figure the
ratio is ~12×.)

## What the panel cannot do

Nothing here is fixable from inside the cluster: `net.core.rmem_max` is a host sysctl,
and changing it needs privileged access to the node, which MatrixCtrl deliberately
does not have. This is a finding it can name and not close. That is the right scope
for a read-only pre-flight check — unlike P1-14, where naming without fixing was
called half a feature, here the fix is one documented command on the host and the
panel's job is to say it is needed and not yet biting.

## Scope

**Ships:** the buffer finding on `/rtc`, carrying LiveKit's own two numbers, the drop
count, and the command that fixes it.

**Does not ship: reading or writing host sysctls.** Not readable correctly from this
pod (see above) and not writable without privilege this project has spent two etappes
narrowing (E37, E40).

**Does not ship: alerting on it.** It is latent by definition; an alert for a
condition that has never fired is how alerts get muted.

## Definition of done

- The finding appears with LiveKit's numbers, not the sysctl's
- It states plainly that nothing has been dropped yet when the counter is zero, and
  says so differently when it is not
- A missing warning line is `unknown`, never `ok` — the line scrolling out of a long
  log is not evidence the buffer is fine
- No `/proc` read from MatrixCtrl's own namespace
- `make check` green
