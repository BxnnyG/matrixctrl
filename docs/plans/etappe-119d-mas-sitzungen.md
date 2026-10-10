# Etappe 119d — MatrixCtrl räumt seine MAS-Sitzungen auf

Stand 2026-10-11, Betreiber-Screenshot: die Sitzungsliste des eigenen MAS-Kontos voller
„Unknown device · MatrixCtrl", eine pro Tag und mehr.

## Befund (gelesen)

MatrixCtrl beendet bei MAS nie eine Sitzung:

- **Anmeldung** (`openid email`): Der Token wird für Userinfo gebraucht, danach hat
  MatrixCtrl seine eigene Sitzung — die MAS-Sitzung bleibt trotzdem offen, für immer.
- **Räume/Moderation verbinden** (`openid` + Client-API + Synapse-Admin): Der Refresh-Token
  liegt nur im Speicher. Abmelden vergisst ihn, ein Neuverbinden ersetzt ihn, ein Neustart
  verliert ihn — beendet wird die Sitzung in keinem der drei Fälle. Seit E52 verbindet
  MatrixCtrl nach jedem Neustart automatisch neu: eine Sitzung mehr pro Neustart.

MAS 1.25 kann aufräumen: Admin-API `GET /api/admin/v1/oauth2-sessions` mit
`filter[client]`, `filter[status]`; `POST …/oauth2-sessions/{id}/finish`.

## Was gebaut wird

1. **Nicht mehr liegen lassen:** Nach der Anmeldung wird die MAS-Sitzung sofort beendet
   (Token-Widerruf, RFC 7009). Ein Matrix-Zugriff, der ersetzt oder beim Abmelden
   vergessen wird, wird vorher widerrufen.
2. **Aufräumen, was liegt** (auch alles Bisherige und was ein Neustart hinterlässt):
   stündlich und beim Start die aktiven Sitzungen *des MatrixCtrl-Clients* —
   - reine Anmelde-Sitzungen (ohne Client-API-Scope), älter als 10 Minuten → beenden;
   - Matrix-Zugriffe, seit 24 h nicht benutzt → beenden (ein benutzter Zugriff erneuert
     sich alle paar Minuten und ist damit nie so alt; ein beendeter wird beim nächsten
     Öffnen der Räume still neu verbunden, E52).
   Nur Sitzungen dieses einen Clients; die Entscheidung als reine Funktion mit Tests.
3. Das Log sagt, wie viele beendet wurden.

## Fertig wenn

- Tests: Anmelde-Sitzung alt/neu, Zugriff benutzt/unbenutzt, fremder Client nie, schon
  beendete nie — je mit Gegenprobe.
- Live: Auflistung gegen die Spielwiese; danach in Produktion die Liste leer bis auf
  aktive Zugriffe.
