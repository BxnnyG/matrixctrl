# Etappe 125 — Einladungs-Codes (MAS-Registrierungstokens)

Stand 2026-11-18. Paket F aus `programm-2026-11.md`. Wer einen geschlossenen Server betreibt und
jemanden einladen will, legt heute entweder das Konto selbst an oder öffnet die Registrierung für
alle. MAS kann Registrierung an einen Code binden — das Panel bietet es nicht an.

## Befund (gelesen)

- MAS-Client: `internal/mas/mas.go` (Admin-Token per client_credentials, Scope `urn:mas:admin`),
  Helfer `get`, `post`, `doPostJSON` (`actions.go`). Keine Registrierungstokens im Repo.
- MAS 1.27 (Spielwiese), Admin-API: `GET/POST /api/admin/v1/user-registration-tokens`
  (Filter `used|revoked|expired|valid`, Seiten per Cursor), `GET/PUT …/{id}`
  (`expires_at`, `usage_limit`, beide nullable), `POST …/{id}/revoke|unrevoke`.
  Anlegen: `token` (leer = zufällig), `usage_limit`, `expires_at`. Seit MAS 0.20 vorhanden.
- Registrierungs-Schalter: Feld `mas.password_registration` in `internal/tasks/tasks.go:151`
  (→ `account.password_registration_enabled`). Code-Pflicht: `account.password_registration_token_required`
  ab MAS 1.19; davor (und in 1.27 noch, veraltet) `account.registration_token_required`.
  Gilt **nur für Passwort-Registrierung**; Upstream-Anbieter haben eigenen Schalter
  (`masupstream.go:156`, bleibt unberührt).
- Benutzer-Seite `web/src/routes/users.tsx`, Gruppe „Menschen"; Routen `internal/api/router.go`
  (Schreiben hinter `RequireWrite`, Audit-Middleware protokolliert Pfad).

## Was gebaut wird

1. **`internal/mas/regtokens.go`**: `ListRegistrationTokens(filter)` (alle Seiten),
   `CreateRegistrationToken(token, usageLimit, expiresAt)`, `EditRegistrationToken`,
   `RevokeRegistrationToken`, `UnrevokeRegistrationToken`. Tests gegen httptest-MAS.
2. **API** `GET /api/v1/invites` (lesende Rollen), `POST /api/v1/invites`,
   `PUT /api/v1/invites/{id}`, `POST /api/v1/invites/{id}/revoke|unrevoke` (`RequireWrite`).
   MAS nicht verbunden → 503 mit verständlichem Text, nie 500. Audit über Middleware;
   der Code selbst landet nicht im Audit.
3. **Einstellung „Registrierung nur mit Code"** als Feld im Aufgaben-Katalog neben
   `mas.password_registration`. Schlüssel nach MAS-Version: ≥ 1.19 → `password_registration_token_required`,
   sonst `registration_token_required`, Version unbekannt → alter Schlüssel (gilt überall).
   Lesen beachtet beide. Hinweis im Feld: wirkt nur, wenn Passwort-Registrierung an ist.
4. **Seite „Einladungen"** (`/invites`, Gruppe „Menschen"): Liste mit Status (gültig, benutzt
   x/y, abgelaufen, widerrufen), Anlegen (Code frei/zufällig, Nutzungen, Ablauf in Tagen),
   Kopieren von Code + Registrierungsadresse, Ablauf/Limit ändern, Widerrufen/Wiederherstellen.
   Oben: Zustand der Registrierung (aus/offen/nur mit Code) mit Link zur Einstellung.

## Randfälle (PROZESS §1)

1 Greenfield/kein MAS-Client → Seite sagt „MAS nicht verbunden", keine Fehlerflut ·
4 MAS langsam → Timeout aus dem Client, Fehler sichtbar · 5 kein Internet: unberührt ·
6 beide Anmeldearten: Routen hinter normaler Sitzung, Client unabhängig vom Login ·
7 Config: Feld fehlt → Standard `false`; Kommentare bleiben (S11 #2) · Code-Pflicht an,
Passwort-Registrierung aus → Hinweis statt stiller Wirkungslosigkeit · 2/3/8: nicht berührt.

## Fertig wenn

Go-Tests (Client: Seiten, Fehler, Felder; Handler: 503 ohne MAS, RequireWrite; Feld: Schlüssel
nach Version, beide lesbar); vitest für die Seite; `make check` grün; Gegenproben rot; rc auf dem
Testcluster: Code anlegen → Registrierung mit Code klappt, ohne Code abgewiesen, widerrufener
Code abgewiesen, Limit greift; `make e2e` + S11. Release v0.1.131 über rc.
