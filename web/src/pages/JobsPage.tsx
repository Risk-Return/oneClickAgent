import { useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { useJob, useSubmitJob, useCancelJob } from "@/features/useJobs";
import { useVisibleSkills } from "@/features/useSkills";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { JobProgressCard } from "@/components/JobProgressCard";
import { FileDropzone } from "@/components/FileDropzone";
import { SkillSelector } from "@/components/SkillSelector";
import { toast } from "sonner";
import { Send, Loader2, Monitor, X } from "lucide-react";

export function JobsPage() {
  const { jobId } = useParams<{ jobId: string }>();
  const navigate = useNavigate();
  const [fileIds, setFileIds] = useState<string[]>([]);
  const [skillId, setSkillId] = useState<string | null>(null);
  const [command, setCommand] = useState("");

  const submitJob = useSubmitJob();
  const { data: job, isLoading: jobLoading } = useJob(jobId || "");
  const cancelJob = useCancelJob();
  const { data: skills } = useVisibleSkills();

  const handleSubmit = () => {
    if (!command.trim()) {
      toast.error("Please enter a command");
      return;
    }

    submitJob.mutate(
      {
        command: command.trim(),
        file_ids: fileIds.length > 0 ? fileIds : undefined,
        skill_id: skillId || undefined,
      },
      {
        onSuccess: (job) => {
          setCommand("");
          setFileIds([]);
          setSkillId(null);
          if (job.id) {
            navigate(`/jobs/${job.id}`);
          }
        },
      }
    );
  };

  const handleCancel = () => {
    if (job?.id) {
      cancelJob.mutate(job.id);
    }
  };

  if (jobId) {
    return (
      <div className="space-y-6 p-6 max-w-2xl">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Job</h1>
          <p className="text-muted-foreground font-mono text-xs">{jobId}</p>
        </div>

        {jobLoading ? (
          <Card>
            <CardContent className="space-y-4 py-6">
              <Skeleton className="h-6 w-32" />
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-8 w-full" />
            </CardContent>
          </Card>
        ) : job ? (
          <div className="space-y-4">
            <Card>
              <CardHeader>
                <CardTitle className="text-base">{job.command}</CardTitle>
                <CardDescription className="text-xs font-mono">ID: {job.id}</CardDescription>
              </CardHeader>
              <CardContent>
                <JobProgressCard
                  status={job.status}
                  percent={job.percent}
                  progressMessage={job.progress_message}
                  queuePosition={job.queue_position}
                  estimatedWaitSeconds={job.estimated_wait_seconds}
                  startedAt={job.started_at}
                  onCancel={handleCancel}
                />
              </CardContent>
            </Card>

            {["succeeded", "failed"].includes(job.status) && job.result && (
              <Card>
                <CardHeader>
                  <CardTitle className="text-base">Result</CardTitle>
                </CardHeader>
                <CardContent>
                  <pre className="whitespace-pre-wrap rounded-md bg-muted p-4 text-sm">
                    {JSON.stringify(job.result, null, 2)}
                  </pre>
                </CardContent>
              </Card>
            )}

            <div className="flex gap-2">
              {!["succeeded", "failed", "cancelled"].includes(job.status) && (
                <Button variant="destructive" onClick={handleCancel} disabled={cancelJob.isPending}>
                  <X className="mr-2 h-4 w-4" /> Cancel job
                </Button>
              )}
              {job.status === "running" && (
                <Button variant="outline">
                  <Monitor className="mr-2 h-4 w-4" /> Open Browser
                </Button>
              )}
            </div>
          </div>
        ) : (
          <Card>
            <CardContent className="flex flex-col items-center gap-4 py-12">
              <p className="text-muted-foreground">Job not found</p>
              <Button variant="outline" onClick={() => navigate("/jobs")}>
                Back to command
              </Button>
            </CardContent>
          </Card>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-6 p-6 max-w-2xl">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Command</h1>
        <p className="text-muted-foreground">Send a job to an AI agent. An agent will be allocated from the pool automatically.</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">New Job</CardTitle>
          <CardDescription>Describe what you want the agent to do.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label>Command</Label>
            <Textarea
              placeholder="e.g., Build a simple React component that..."
              value={command}
              onChange={(e) => setCommand(e.target.value)}
              rows={3}
            />
          </div>

          <FileDropzone
            fileIds={fileIds}
            onFilesChange={setFileIds}
            disabled={submitJob.isPending}
          />

          {skills && skills.length > 0 && (
            <SkillSelector
              skills={skills}
              selectedSkillId={skillId}
              onSkillChange={setSkillId}
            />
          )}

          <Button
            onClick={handleSubmit}
            disabled={submitJob.isPending || !command.trim()}
            className="w-full"
          >
            {submitJob.isPending ? (
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
            ) : (
              <Send className="mr-2 h-4 w-4" />
            )}
            Send job
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
