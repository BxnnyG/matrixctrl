# Etappe 119a — Wenn der Server keine Namen mehr auflösen kann

Stand 2026-10-10, Betreiber-Meldung vom Produktionsserver:

> token exchange: Post "https://mas-matrix…/oauth2/token": dial tcp: lookup mas-matrix… on
> 10.43.0.10:53: server misbehaving
> (MAS) error sending request for url (https://auth…/oauth/v2/token) … dns error

## Befund (gelesen)

NetBird hatte `/etc/resolv.conf` des Hosts übernommen und schickte **alle** Anfragen an zwei
andere NetBird-Geräte; beide waren offline. CoreDNS gibt an den Host-Resolver weiter → jede
externe Auflösung im Cluster lief in den Timeout: MatrixCtrl → MAS, MAS → Zitadel,
Föderation. Gelöst auf NetBird-Seite; der Cluster hängt weiter daran.

MatrixCtrl hat dazu nur die rohe Go-Fehlermeldung gezeigt, auf der Anmeldeseite — genau
dort, wo man in diesem Zustand festsitzt: Der Notzugang ist gesperrt, solange der
Matrix-Login eingerichtet ist, und der Matrix-Login braucht DNS.

## Was gebaut wird

1. **`internal/dnshealth`**: jede Minute die konfigurierten Namen (Server-Name, Synapse,
   MAS, … und die Anbieter für externe Anmeldung) über den Resolver des Pods — also über
   CoreDNS, wie jeder Dienst im Cluster. Scheitern sie mit Timeout/SERVFAIL, wird derselbe
   Name einmal direkt bei einem öffentlichen Resolver gefragt: Antwortet der, hängt nur die
   Namensauflösung des Clusters (Ursache: der DNS-Server, an den der Host weitergibt);
   antwortet auch der nicht, fehlt die Verbindung nach draußen. Ein Name, der nicht
   existiert (NXDOMAIN), ist kein Ausfall, sondern ein DNS-Eintrag — das bleibt Sache von
   TLS & DNS.
2. **Dashboard:** rote Karte mit Ursache, seit wann, welche Namen, und dem Weg, den Cluster
   davon unabhängig zu machen (k3s `resolv-conf`).
3. **Anmeldeseite:** Die Fehlermeldung des Matrix-Logins wird übersetzt („Der Server kann
   den Namen der Anmeldung gerade nicht auflösen — DNS …"), und der öffentliche
   Verfügbarkeits-Endpunkt sagt ein Wort mehr (`dns: failing`), damit die Seite den Grund
   schon vor dem Klick nennt. Keine Adressen, keine Details vor der Anmeldung.

## Fertig wenn

- Tests: Einordnung der Fehler (Timeout, SERVFAIL, NXDOMAIN); Urteil „nur Cluster-DNS"
  vs. „kein Netz"; NXDOMAIN allein ist kein Ausfall; die übersetzte Anmeldemeldung — je mit
  Gegenprobe.
- Live: Prüfung gegen einen absichtlich toten Resolver und gegen den echten.
