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
  errorCode: string | null;
  errorMessage: string | null;
  onCancel: () => void;
  onResubmit?: () => void;
}

export function JobProgressCard({
  status,
  percent,
  progressMessage,
  queuePosition,
  estimatedWaitSeconds,
  startedAt,
  errorCode,
  errorMessage: _errorMessage,
}: JobProgressCardProps) {
  const isActive = status === "running" || status === "dispatched";
  const isExpired = errorCode === "QUEUE_TIMEOUT";

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

      {isExpired && (
        <div className="rounded-md bg-destructive/10 p-3 text-sm text-destructive">
          Job expired in queue. Cancel or resubmit.
        </div>
      )}

      <div aria-live="polite" aria-atomic="true">
        <Progress value={percent} className={status === "failed" ? "[&>div]:bg-red-500" : ""} />
        <p className="mt-2 text-sm text-muted-foreground">{progressMessage || "Waiting..."}</p>
      </div>
    </div>
  );
}
