import { Progress } from "@/components/ui/progress";
import { Badge } from "@/components/ui/badge";
import { AgentStatusBadge } from "./AgentStatusBadge";
import { formatDistanceToNow } from "date-fns";

interface JobProgressCardProps {
  status: string;
  percent: number;
  progressMessage: string | null;
  queuePosition: number | null;
  estimatedWaitSeconds: number | null;
  startedAt: string | null;
  onCancel: () => void;
}

export function JobProgressCard({
  status,
  percent,
  progressMessage,
  queuePosition,
  estimatedWaitSeconds,
  startedAt,
}: JobProgressCardProps) {
  const isActive = status === "running" || status === "dispatched";

  const elapsed = startedAt
    ? formatDistanceToNow(new Date(startedAt), { addSuffix: false })
    : null;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <AgentStatusBadge status={status} />
          {isActive && elapsed && (
            <span className="text-sm text-muted-foreground">{elapsed}</span>
          )}
        </div>
      </div>

      {status === "queued" && (
        <div className="flex items-center gap-2">
          <Badge variant="warning">In Queue</Badge>
          {queuePosition != null && (
            <span className="text-sm text-muted-foreground">
              #{queuePosition} in queue
              {estimatedWaitSeconds != null &&
                ` · ~${Math.ceil(estimatedWaitSeconds / 60)} min wait`}
            </span>
          )}
        </div>
      )}

      <Progress value={percent} className={status === "failed" ? "[&>div]:bg-red-500" : ""} />
      <p className="text-sm text-muted-foreground">{progressMessage || "Waiting..."}</p>
    </div>
  );
}
