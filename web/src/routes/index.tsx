import { createFileRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { ComponentCard } from "@/components/status/ComponentCard";
import { ReleaseCard } from "@/components/status/ReleaseCard";

export const Route = createFileRoute("/")({
  component: Dashboard,
});

interface ComponentStatus {
  name: string;
  status: string;
  ready: number;
  desired: number;
  restarts: number;
}

interface StatusResponse {
  release?: {
    name: string;
    chart_version: string;
    revision: number;
    status: string;
    deployed_at?: string;
  };
  components: ComponentStatus[];
}

function Dashboard() {
  const { data: status, isLoading } = useQuery({
    queryKey: ["status"],
    queryFn: () => api.get<StatusResponse>("/api/v1/status"),
    refetchInterval: 15_000,
  });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">Dashboard</h1>

      {isLoading && (
        <div className="text-sm text-gray-500">Lade Status...</div>
      )}

      {status && (
        <>
          <ReleaseCard release={status.release} />

          <section>
            <h2 className="text-lg font-medium mb-3">Komponenten</h2>
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
              {status.components?.map((c) => (
                <ComponentCard key={c.name} component={c} />
              ))}
            </div>
          </section>
        </>
      )}
    </div>
  );
}
