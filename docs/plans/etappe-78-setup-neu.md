# Etappen 78–84 — Das Setup, von vorne gedacht

## Auslöser

Ein Operator hat einen zweiten Server aufgesetzt und wollte vom ersten umziehen.
Sein Bericht, gekürzt:

> „setup gibts keine option mit backup wieder einspielen, also muss ich dann auf
> setup matrix deployen und dann backup wieder einspielen kann ich das voher
> zeigt die ui nicht steht nirgends wiso kann er nicht die ess version aus backup
> ziehen und deployen?!?! […] er fragt mich nach mas url issuer, aber wenn man es
> dadurch doch deployed wiso sollte ich die angeben […] wiso zeigt setup nicht
> an bitte lege folgenden records an, check records ob die resolven […] und ggf
> trotzdem weiter machen quasi overide […] dann wo sehe ich die matrixctrl
> version? wo sehe ich ob die geupdated werden kann … wie updatet man die"

Dazu ein Screenshot: `Something went wrong!` mit `Minified React error #310`.

## Befunde (nachgesehen, nicht vermutet)

| # | Befund | Beleg |
|---|---|---|
| 1 | Config-Tab stürzt bei kaltem Cache ab | `web/src/routes/config/index.tsx:136` — `useQuery` nach zwei frühen `return`s. React `#310` = „Rendered more hooks than during the previous render" (Reacts eigene `codes.json`) |
| 2 | Der Linter, der das findet, läuft nirgends | `eslint.config.js` bindet `reactHooks.configs.flat.recommended` ein. `eslint .` meldet die Stelle wörtlich. Weder `make check` noch CI rufen ihn auf. 59 der 78 Fehler waren `react-refresh/only-export-components` — das Signal lag unter dem Rauschen |
| 3 | Kein Wiederherstellungs-Pfad im Setup | `setup.tsx` bietet `DeployWizard` und `AdoptCard`. Kein dritter Weg |
| 4 | Die ESS-Version steht im Archiv und wird nicht benutzt | `backup.Manifest.ESS` trägt Version + Revision, die Vorschau **zeigt** sie („ESS 26.8.0 (Revision 30)") — nichts handelt danach |
| 5 | Nach Ableitbarem gefragt | `connect-oidc` verlangt `issuer` + `public_url` als leere Felder. Bei Greenfield hat MatrixCtrl den Server-Namen selbst gesetzt und leitet `mas.<serverName>` ab |
| 6 | DNS unsichtbar | Ein 11px-Hinweis „Hostnames werden abgeleitet: matrix., mas., element., admin., mrtc." Keine Record-Tabelle, kein Ziel, keine Prüfung, kein Override |
| 7 | Eigene Version nirgends | `version.Version` geht in die Startzeile und ins Backup-Manifest. Kein Endpunkt, keine Anzeige, keine Update-Prüfung, kein Update-Weg in der UI |

## Leitsatz

**Setup sagt jederzeit: was ich weiß, was ich brauche, und wer es tun muss.**

Die drei Fehlerklassen oben sind genau die drei Verstöße dagegen: nach Ableitbarem
fragen (5), Notwendiges verschweigen (6), einen existierenden Zustand nicht
vorsehen (3, 4).

## Etappen

| # | Titel | Größe | Hängt ab von |
|---|---|---|---|
| 78 | Der Config-Tab, der abstürzt — und der Linter, der das seit jeher weiß | S | — |
| 79 | Welche Version läuft hier, gibt es eine neuere, wie kommt sie her | S | — |
| 80 | DNS als eigener Schritt: Records, Prüfung, Override | M | — |
| 81 | Nicht fragen, was ableitbar ist — plus Vorschau vor dem Deploy | M | 80 |
| 82 | Umzug: Wiederherstellen als dritter Setup-Pfad | L | 80, 81 |
| 83 | Ein geführter Ablauf statt drei Karten | M | 80, 81, 82 |
| 84 | Self-Update per Kubernetes-Job | M | 79 |

### E78 — erledigt, v0.1.73

Hook über die frühen `return`s gezogen. `react-refresh/only-export-components`
aus (Dev-Server-Ergonomie, keine Korrektheit), `react-hooks/rules-of-hooks` auf
`error`, die übrigen react-hooks-Regeln auf `warn`, damit das Gate **heute**
scharf ist statt nach einer Aufräumaktion, die niemand terminiert hat.
`eslint .` in `make check`. Gegenprobe: Hook zurückgeschoben → Gate exit 1 mit
genau dieser Meldung.

### E79 — Version, Update-Verfügbarkeit, Update-Weg

- `GET /api/v1/version` → `{version, commit, built_at}`. Existiert nicht.
- Anzeige in der Shell-Fußzeile, neben der ESS-Chart-Version, die dort schon steht.
- Update-Prüfung gegen die GHCR-Tag-Liste (anonymes Token, derselbe Aufruf, mit dem
  ich Releases verifiziere), SemVer-Vergleich, Ergebnis gecacht.
- Update-Weg: der exakte Befehl, kopierbar. Kein Selbst-Upgrade in dieser Etappe —
  siehe E84, warum das nicht nebenbei geht.

### E80 — DNS

- `GET /api/v1/setup/dns?server_name=…` → je Record: Name, erwartetes Ziel,
  tatsächlich aufgelöste Adressen, Übereinstimmung.
- Ziel-IP: `NodeInfo` liefert `ExternalIP`, sonst `InternalIP`. Hinter NAT kennt
  MatrixCtrl die öffentliche Adresse nicht — dann wird sie **erfragt** und als
  „vom Operator angegeben" gekennzeichnet, statt eine private als Wahrheit zu zeigen.
- Tabelle mit Kopierknopf, „Erneut prüfen", und **„Trotzdem fortfahren"** als
  beschrifteter Override. DNS-Propagation dauert; ein Setup, das darauf besteht,
  ist ein Setup, das man umgeht.

### E81 — Ableiten statt fragen

- `connect-oidc` bekommt Vorbelegung: `issuer = https://mas.<serverName>` aus den
  ESS-Werten, `public_url` aus dem eigenen Ingress-Host. Beides editierbar hinter
  „abweichend konfigurieren" — sichtbar bleibt, was gilt.
- Vorschau vor dem Deploy: welcher Chart, welche Version, welche Hostnames, welche
  Werte weichen von den Defaults ab. Der Profi will sehen, was passiert, bevor es
  passiert; der Ahnungslose liest die Zeile mit dem Server-Namen und merkt den Tippfehler.

### E82 — Umzug

Der dritte Pfad kehrt die Reihenfolge um: **erst das Archiv, dann alles andere.**

1. Archiv hochladen. `restore/preview` gibt es bereits.
2. Aus dem Manifest einen **Plan** bauen statt einer Anzeige: ESS-Version → wird so
   deployed · Server-Name aus der Config → übernommen · 71 Dateien / 14 Tabellen →
   nach dem Deploy eingespielt. Jede Zeile überschreibbar.
3. DNS-Schritt (E80) mit den Namen aus dem Archiv.
4. Deploy der Version aus dem Archiv.
5. Restore automatisch im Anschluss, mit Fortschritt.

Neu im Backend: `POST /api/v1/setup/plan-from-archive` (Manifest → Plan) und die
Verkettung Deploy → Restore als ein Vorgang mit einem Fortschritts-Stream.

### E83 — Ein geführter Ablauf

Setup wird ein Zustandsautomat mit sichtbarer Schrittleiste statt drei
unverbundener Karten. Der Zustand kommt aus dem, was schon da ist
(`ess_installed`, `config_sections`, `oidc_configured`) plus dem gewählten Pfad.
Diese Etappe baut nichts Neues — sie ordnet 80–82 zu einem Bildschirm, auf dem
jederzeit steht, wo man ist und was fehlt.

### E84 — Self-Update

Ein `helm upgrade` auf die eigene Release ersetzt den Pod, der es ausführt. Das
geht nur außerhalb: ein kurzlebiger Kubernetes-Job mit demselben ServiceAccount,
der das Upgrade fährt; die UI pollt, bis die neue Version antwortet. Eigene
Etappe, weil hier ein Fehlschlag den Admin-Zugang kostet und ein Rollback-Pfad
dazugehört.

## Offene Entscheidungen (gehören dem Operator)

1. **Öffentliche IP** hinter NAT: einmal erfragen und merken, oder einen externen
   Dienst fragen (Egress + Dritter)?
2. **DNS-Auflösung** über den Cluster-Resolver (CoreDNS, kennt Split-Horizon) oder
   fest gegen einen öffentlichen Resolver?
3. **Update-Prüfung**: darf der Cluster GHCR erreichen? Opt-in oder Opt-out?
4. **Self-Update** (E84) gewünscht, oder reicht der kopierbare Befehl?
5. **Restore ohne Deploy**: soll der Umzugs-Pfad auch Config-only auf ein bereits
   laufendes ESS können?
