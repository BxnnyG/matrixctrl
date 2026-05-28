import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";

export const Route = createFileRoute("/auth/callback")({
  component: OIDCCallback,
});

function OIDCCallback() {
  const navigate = useNavigate();

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const token = params.get("token");
    const error = params.get("error");

    if (token) {
      localStorage.setItem("matrixctrl_token", token);
      // Clean the token out of the URL before navigating
      window.history.replaceState({}, "", "/auth/callback");
      navigate({ to: "/" });
    } else {
      // Forward any error to the login page
      const dest = error ? `/auth/login?error=${encodeURIComponent(error)}` : "/auth/login";
      navigate({ to: dest as "/" });
    }
  }, [navigate]);

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50 dark:bg-gray-950">
      <p className="text-sm text-gray-500 dark:text-gray-400">Anmeldung wird abgeschlossen…</p>
    </div>
  );
}
