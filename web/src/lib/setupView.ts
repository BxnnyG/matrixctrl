/** Which card Setup shows, as a function of the status — and of what this page itself
 *  has set running.
 *
 *  "busy" exists for operations someone else started: a second Helm operation fails on
 *  the first, so the page waits instead of offering one. It was also shown for the
 *  page's own: connecting the Matrix login runs an upgrade, the release goes
 *  pending-upgrade, and the next status poll replaced the connect card — its log, its
 *  result — with "ESS wird gerade installiert … another operation is in progress".
 *  The connect had worked; the operator saw a foreign error and, a minute later, the
 *  connect button again (etappe 116b). The deploy and move wizards had the same
 *  exposure through pending-install.
 *
 *  So a view that started something is held until that something ends. */

export type SetupView = "busy" | "failed" | "install" | "adopt" | "connect" | "connected";

export interface SetupViewInput {
  ess_state?: "absent" | "busy" | "deployed" | "failed";
  ess_installed: boolean;
  config_sections: number;
  oidc_configured: boolean;
}

export function setupView(s: SetupViewInput): SetupView {
  if (s.ess_state === "busy") return "busy";
  if (s.ess_state === "failed") return "failed";
  if (!s.ess_installed) return "install";
  if (s.config_sections === 0) return "adopt";
  if (!s.oidc_configured) return "connect";
  return "connected";
}

/** The view to render. `held` is the view that was on screen when this page started an
 *  operation, or null when nothing it started is running. */
export function shownView(s: SetupViewInput, held: SetupView | null): SetupView {
  return held ?? setupView(s);
}
