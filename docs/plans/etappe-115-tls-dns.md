# Etappe 115 — TLS & DNS

Stand 2026-09-29. Bisher ein ausgegrauter Eintrag in der Navigation.

## Warum, aus drei echten Fällen (26.–28.09.)

| Fall | Symptom | Was eine Seite gezeigt hätte |
|---|---|---|
| `rtcmatrix` grau in Cloudflare | Element Call: `OPEN_ID_ERROR` | Zertifikat am Ursprung = „TRAEFIK DEFAULT CERT", selbstsigniert |
| danach orange | Anrufe gingen, aber der Auth-Dienst lief ins Timeout | von außen gültig, aus dem Cluster nicht erreichbar |
| Umzug | alle Namen zeigten noch auf den alten Server | DNS zeigt woanders hin |

Der Wert liegt in **zwei Blickrichtungen**: was das Internet sieht, und was am Ursprung
steht. Cloudflare verdeckt die zweite — und genau dort lag jedes Mal der Fehler.

## Was gebaut wird

1. **`internal/tlscheck`** — ein TLS-Handshake mit SNI, wahlweise gegen eine bestimmte
   Adresse (Ursprung) oder gegen den Namen (öffentlich): Aussteller, Ablauf, Namen im
   Zertifikat, selbstsigniert ja/nein. Kein Zertifikat wird akzeptiert oder gespeichert;
   die Prüfung liest nur.
2. **Seite „TLS & DNS"** — je Hostname aus den Einstellungen (nicht geraten):
   - **DNS:** worauf der Name zeigt, und ob das dieser Server ist (`dnscheck`).
   - **Über Cloudflare?** erkannt am `server`-Header der Antwort, nicht an einer
     IP-Liste, die veraltet.
   - **Zertifikat von außen:** Aussteller, Restlaufzeit, passt der Name.
   - **Zertifikat am Ursprung:** derselbe Handshake gegen den Ingress-Controller im
     Cluster. Zeigt selbstsignierte Zertifikate, die Cloudflare verdeckt.
3. **Eine Zeile Klartext je Hostname**, statt vier Felder zum Selbstdeuten.

## Randfälle

- Kein Cluster-Zugriff → Ursprungsprüfung entfällt, wird gesagt.
- Name zeigt auf Cloudflare *und* der Ursprung ist selbstsigniert: kein Fehler, sondern
  der Normalfall bei „Flexible/Full"-Verschlüsselung — aber genannt, weil er bei
  Cluster-internen Aufrufen zuschlägt (§4.112: der Auth-Dienst).
- Alles mit Zeitlimit; ein hängender Name darf die Seite nicht blockieren.
- Ablauf in unter 14 Tagen wird hervorgehoben.

## Fertig wenn

- Für jeden konfigurierten Hostnamen steht da, worauf er zeigt und welches Zertifikat
  außen und am Ursprung ausgeliefert wird.
- Ein selbstsigniertes Ursprungszertifikat wird als solches benannt (Test).
- Die Seite ist in der Navigation nicht mehr ausgegraut.
