import { useQuery } from "@tanstack/react-query";
import { api } from "./api";

/** Who this session is — not which login the instance offers.
 *
 *  Those used to be one question on screen. After connecting the Matrix login, the
 *  instance offers it, while the tab that did the connecting is still signed in with
 *  the emergency login — and the sidebar read "Matrix-Login" for a session that was
 *  not one (etappe 116b). */
export const BOOTSTRAP_USER = "admin"; // internal/auth.BootstrapUserID

export function useSession() {
  const q = useQuery({
    queryKey: ["auth", "me"],
    queryFn: () => api.get<{ user_id: string }>("/api/v1/auth/me"),
    staleTime: 60_000,
  });
  const userId = q.data?.user_id;
  return {
    userId,
    bootstrap: userId === BOOTSTRAP_USER,
    /** "@alice:example.com" → "alice"; the emergency login → "Admin". */
    name: !userId || userId === BOOTSTRAP_USER ? "Admin" : userId.replace(/^@/, "").split(":")[0],
  };
}

/** Sign out for real: the server revokes the session, then the tab forgets it.
 *
 *  The button used to do only the second half, which left a valid session behind on
 *  the server for whoever held a copy of the token. */
export async function signOut() {
  try {
    await api.post("/api/v1/auth/logout", {});
  } catch { /* already gone, or the server is away — the local half still applies */ }
  localStorage.removeItem("matrixctrl_token");
  window.location.href = "/auth/login";
}
