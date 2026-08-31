# Etappe 56 — Memory, without crying wolf

E55 shipped the capacity preflight for CPU only, and said why: memory overcommit is
normal, the kernel reclaims, and a warning tuned like the CPU one would fire constantly
and be ignored. The operator has asked for memory anyway — correctly, because that
reasoning was an argument against *one particular threshold*, not against measuring
memory at all.

## The distinction that makes it safe

E55 already reports two different things:

- **exceeds the largest node** — no node can ever run this pod
- **exceeds what is currently free** — the cluster is busy right now

The second is where memory would cry wolf: a node with 36 GiB and 30 GiB requested is
completely normal, and Kubernetes is designed to run that way. The first is not a
matter of tuning at all — a pod requesting 64 GiB on a 36 GiB node is unschedulable as
a matter of arithmetic, exactly as it is for CPU.

So memory ships with **only the blocking check**, and CPU keeps both. Not a compromise:
the two resources behave differently under pressure and the check should say only what
it can stand behind.

## Scope

**Ships:** memory in the "larger than any node" verdict, with its own message, and a
finding that can name both resources when both are exceeded.

**Does not ship: a memory pressure warning.** The `currently free` half stays
CPU-only, for the reason above. If it is ever wanted it needs a different threshold
derived from real behaviour, not the CPU one copied across.

## Definition of done

- A pod requesting more memory than any node has is reported blocking
- A pod merely exceeding *free* memory is not reported at all
- A pod exceeding both CPU and memory says so once, naming both
- The existing CPU behaviour is unchanged, asserted by the tests E55 already wrote
- `make check` green
