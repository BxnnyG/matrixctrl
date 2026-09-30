import { useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { signOut } from "@/lib/session";
import { Button, Card, Icon } from "@/components/mc";

/** Whether MatrixCtrl can sign in through MAS at all. Rooms, moderation and users all
 *  depend on it; asked once, shared through the query cache. */
export function useMatrixLogin() {
  return useQuery({
    queryKey: ["auth", "oidc", "available"],
    queryFn: () => api.get<{ enabled: boolean; retrying?: boolean }>("/api/v1/auth/oidc/available"),
    staleTime: 60_000,
  });
}

/** Shown instead of a button that cannot work (etappe 116a).
 *
 *  After a move to a new server MatrixCtrl ran in bootstrap mode, and the rooms and
 *  moderation pages offered "Verbinden" — which asked the server for a login address,
 *  got 501 "not configured", and showed nothing. The operator saw a spinner and then
 *  the same button. This says what is missing and where it is fixed. */
export function MatrixLoginMissing({ what }: { what: string }) {
  const navigate = useNavigate();
  return (
    <Card>
      <div style={{ padding: 24, display: "flex", flexDirection: "column", gap: 12, alignItems: "flex-start" }}>
        <div style={{ display: "flex", alignItems: "center", gap: 9 }}>
          <Icon name="key" size={17} style={{ color: "var(--status-warn)" }} />
          <span style={{ fontSize: 14.5, fontWeight: 650, color: "var(--text)" }}>MatrixCtrl ist noch nicht mit dem Matrix-Login verbunden</span>
        </div>
        <p style={{ margin: 0, fontSize: 13, color: "var(--text-dim)", lineHeight: 1.65, maxWidth: 640 }}>
          {what} braucht die Anmeldung über MAS. Im Moment läuft MatrixCtrl im Notzugang (Bootstrap) —
          typisch nach einer Neuinstallation oder einem Umzug. Das Verbinden registriert MatrixCtrl bei MAS,
          startet MAS einmal neu und schaltet die Anmeldung erst um, wenn MAS bestätigt hat und ein Admin-Konto existiert.
        </p>
        <Button variant="primary" size="sm" icon="key" onClick={() => navigate({ to: "/setup" })}>Im Setup verbinden</Button>
      </div>
    </Card>
  );
}

/** The step after connecting, as a button instead of a sentence. */
export function SignInWithMatrix() {
  return (
    <Button variant="primary" size="sm" icon="logout" onClick={() => void signOut()}>
      Abmelden und über Matrix anmelden
    </Button>
  );
}

/** Shown to an emergency-login session on the screens that act with a Matrix account.
 *
 *  The Matrix login was connected, the operator was still in the tab that connected it,
 *  and rooms offered "Verbinden": the authorization went through MAS, filed the token
 *  under the Matrix account, and this session — which is not that account — asked
 *  again. Round and round, with nothing on screen (etappe 116d). */
export function EmergencySessionNeedsMatrix({ what }: { what: string }) {
  return (
    <Card>
      <div style={{ padding: 24, display: "flex", flexDirection: "column", gap: 12, alignItems: "flex-start" }}>
        <div style={{ display: "flex", alignItems: "center", gap: 9 }}>
          <Icon name="key" size={17} style={{ color: "var(--status-warn)" }} />
          <span style={{ fontSize: 14.5, fontWeight: 650, color: "var(--text)" }}>Du bist mit dem Notzugang angemeldet</span>
        </div>
        <p style={{ margin: 0, fontSize: 13, color: "var(--text-dim)", lineHeight: 1.65, maxWidth: 640 }}>
          {what} handelt mit deinem Matrix-Konto — mit genau den Rechten, die du selbst hast. Der Notzugang
          ist kein Matrix-Konto. Der Matrix-Login ist eingerichtet: einmal abmelden und über Matrix anmelden,
          dann geht es hier weiter.
        </p>
        <SignInWithMatrix />
      </div>
    </Card>
  );
}

