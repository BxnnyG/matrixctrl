# Etappe 71 — Where the configuration actually lives

The operator, on seeing the backup work:

> aso und natürlich auch dann schönner und besser speichert als ich das gemacht habe,
> bei liegts in irgendeinem ordner … am beste wäre das wenn es dann in opt landet wie
> art compose oder halt in volume ist idk — und auch alles geil einstellbar
>
> *(and, on my first reading it as being about backups)* bruder ich meinte die configs!!!

## The answer is better than they think, and that is the problem

Their ESS values sit in `/root/ess-config-values`, and they assume MatrixCtrl keeps
things the same way. It does not:

| | |
|---|---|
| where | `/data/config-repo` on its own PVC (`matrixctrl-config`, 1 Gi, `local-path`) |
| what | a **git repository** — 968 KB, full history, diffs, rollback |
| `/root/ess-config-values` | the **seed** only, read once at first start |

So the thing they asked for largely exists. They could not know, because **nothing in
the panel ever says where the configuration is kept.** Ten screens about the
configuration, and not one of them names the volume it lives on.

That is a documentation failure of a specific kind: not a stale document, but an absent
sentence about the product's own most reassuring property. The operator has been
editing configuration for months believing it lands wherever their old folder was.

## The second half is real

`MATRIXCTRL_CONFIG_REPO` is read from the environment by the Go code, and the chart
template **hardcodes** it:

```yaml
- name: MATRIXCTRL_CONFIG_REPO
  value: /data/config-repo
```

So "einstellbar" is half-true: the size and storage class of the volume are values, the
path is not. That is exactly the shape §4.61 keeps finding — something the code supports
and nothing exposes.

## Scope

**Ships:** the configuration screen states where the repository lives, how large it is
and how many commits it holds; and the chart exposes the path it had been hardcoding.

**Does not ship: moving the config to `/opt` on the host.** It is what "like a compose
setup" would mean literally, and it would be a step backwards — a hostPath ties the pod
to one node, survives no migration, and is exactly what a PVC exists to avoid. The
right answer to "it lies in some folder" is a volume, which is what it already is. Said
plainly rather than implemented, because the request behind it is *knowing where it is*,
and that is the part that was missing.

**Does not ship: a storage browser.** The git history is already visible on the
configuration screen; a file listing would be a second, worse view of the same thing.

## Definition of done

- The configuration screen names the path, the volume and the number of commits
- It distinguishes the live repository from the seed directory, so the old folder stops
  looking like the source of truth
- `storage.config.path` exists in the chart values and the template uses it
- `make check` green
