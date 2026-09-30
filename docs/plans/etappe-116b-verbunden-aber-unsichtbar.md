# Etappe 116b — Verbunden, aber die Seite weiß es nicht

Stand 2026-09-30, Betreiber-Meldung vom neuen Server nach 0.1.115:

> erst hat er geladen, dann sagte er „another …", und dann war wieder der Button mit
> Verbinden da, aber jetzt macht er nix mehr

## Befund (aus dem Log des Servers, nicht vermutet)

Die Verbindung hat funktioniert: `connect-oidc` → 202, Upgrade Revision 11 durchgelaufen,
07:52:25 „OIDC hot-reloaded". Was der Betreiber sah, waren drei Anzeigefehler:

1. **Die Seite hat ihr eigenes Upgrade verdrängt.** Setup fragt alle 30 s
   `/setup/status`. Während des Upgrades steht das ESS-Release auf `pending-upgrade` →
   `ess_state: "busy"` → Setup ersetzt die ganze Ansicht durch „ESS wird gerade installiert
   … another operation is in progress". Die Verbinden-Karte mit ihrem Log verschwindet
   mitten im Lauf, samt Ergebnis. Dasselbe trifft den Deploy- und den Umzugs-Assistenten:
   deren eigene Installation macht das Release ebenso `pending-install`.
2. **Setup kennt den Login-Zustand nur vom Start.** `SetupHandler.oidcConfigured` ist ein
   bool, beim Start gesetzt. Nach dem Hot-Reload meldet `/setup/status` weiter
   `oidc_configured: false` → die Verbinden-Karte kommt zurück, obwohl alles verbunden ist.
3. **Der Knopf antwortet, die Karte zeigt es nicht.** Ein zweites Verbinden landet
   (richtig) bei „bereits vollständig registriert" — 200 ohne `upgrade_id`. Die Karte
   setzt `runId = undefined` und bleibt, wie sie ist. Fünf Klicks, fünf Antworten, nichts
   zu sehen.

## Was gebaut wird

1. `oidc_configured` wird bei jeder Anfrage gefragt, nicht beim Start gemerkt
   (`func() bool` vom AuthHandler, derselbe Lock wie der Hot-Reload).
2. Setup hält die Ansicht fest, solange eine Operation läuft, die diese Seite selbst
   gestartet hat. Der „busy"-Zustand ist für fremde Operationen da, nicht für die eigene.
   Danach wird der Status neu gelesen.
3. Die Verbinden-Karte zeigt jede Antwort ohne Lauf als Meldung an.
4. Nach erfolgreichem Verbinden: ein Knopf „Abmelden und über Matrix anmelden", statt nur
   eines Satzes.

## Fertig wenn

- Test: Status meldet `oidc_configured` nach einem Wechsel zur Laufzeit richtig
  (Gegenprobe: mit dem gemerkten bool fällt er durch).
- Der Setup-Schalter zwischen „busy" und eigener Operation ist eine reine Funktion mit
  Test (Gegenprobe: ohne Festhalten wird „busy" gezeigt).
- Erneutes Verbinden zeigt „bereits vollständig registriert" an.
