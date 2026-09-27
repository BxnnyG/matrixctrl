import { createFileRoute, redirect } from "@tanstack/react-router";

// There used to be a second, complete YAML editor here — with validation, a commit
// message, a diff and deploy — that nothing in the product linked to, while the editor
// people could actually reach had none of that (etappe 107). Its checks now live in the
// YAML mode of the settings page; this route only keeps old links working.
export const Route = createFileRoute("/config/$slice")({
  beforeLoad: ({ params }) => {
    throw redirect({ to: "/config", search: { section: `${params.slice}.yaml`, mode: "yaml" } });
  },
});
