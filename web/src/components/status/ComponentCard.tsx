import { CheckCircle, XCircle, AlertTriangle, Clock } from "lucide-react";

interface Component {
  name: string;
  status: string;
  ready: number;
  desired: number;
  restarts: number;
}

export function ComponentCard({ component: c }: { component: Component }) {
  const healthy = c.ready === c.desired && c.desired > 0;
  const degraded = c.ready > 0 && c.ready < c.desired;

  return (
    <div className="bg-white border border-gray-200 rounded-xl p-4">
      <div className="flex items-start justify-between mb-2">
        <span className="text-sm font-medium text-gray-900 truncate pr-2">
          {c.name}
        </span>
        {healthy ? (
          <CheckCircle className="w-4 h-4 text-green-500 shrink-0" />
        ) : degraded ? (
          <AlertTriangle className="w-4 h-4 text-yellow-500 shrink-0" />
        ) : c.desired === 0 ? (
          <Clock className="w-4 h-4 text-gray-400 shrink-0" />
        ) : (
          <XCircle className="w-4 h-4 text-red-500 shrink-0" />
        )}
      </div>
      <div className="text-xs text-gray-500">
        {c.ready}/{c.desired} Ready
        {c.restarts > 0 && (
          <span className="ml-2 text-orange-500">{c.restarts} Restarts</span>
        )}
      </div>
    </div>
  );
}
