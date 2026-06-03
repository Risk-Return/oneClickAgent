import { useState, useEffect, useCallback } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useJob, useSubmitJob, useCancelJob } from "@/features/useJobs";
import { useVisibleSkills } from "@/features/useSkills";
import { useCredentials, useOpenVNC } from "@/features/useCredentials";
import { getWSClient } from "@/api/ws";
import { apiClient } from "@/api/client";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { JobProgressCard } from "@/components/JobProgressCard";
import { FileDropzone } from "@/components/FileDropzone";
import { SkillSelector } from "@/components/SkillSelector";
import { VNCPanel } from "@/components/VNCPanel";
import { toast } from "sonner";
import { Send, Loader2, Monitor, X, Key, Download } from "lucide-react";
import type { Job, JobStatus } from "@/api/schemas";

function downloadFile(content: string, filename: string, mime = "application/json") {
  const blob = new Blob([content], { type: mime });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

function formatResultContent(result: Record<string, unknown>): string {
  return JSON.stringify(result, null, 2);
}

export function JobsPage() {
  const { t } = useTranslation();
  const { jobId } = useParams<{ jobId: string }>();
  const navigate = useNavigate();
  const [fileIds, setFileIds] = useState<string[]>([]);
  const [skillId, setSkillId] = useState<string | null>(null);
  const [credentialIds, setCredentialIds] = useState<string[]>([]);
  const [command, setCommand] = useState("");
  const [inlineError, setInlineError] = useState<string | null>(null);
  const [vncOpen, setVncOpen] = useState(false);
  const [vncData, setVncData] = useState<{ wsUrl: string; rfbPassword: string; sessionId: string } | null>(null);

  const submitJob = useSubmitJob();
  const cancelJob = useCancelJob();
  const openVNC = useOpenVNC();
  const { data: skills } = useVisibleSkills();
  const { data: credentials } = useCredentials();

  const [liveJob, setLiveJob] = useState<Job | null>(null);
  const { data: job, isLoading: jobLoading } = useJob(jobId || "");

  useEffect(() => {
    if (job && jobId) setLiveJob(job);
  }, [job, jobId]);

  useEffect(() => {
    if (!jobId) return;
    const ws = getWSClient();
    ws.connect();

    ws.subscribe(`job:${jobId}`, (event) => {
      if (event.type === "job.progress") {
        setLiveJob((prev) => {
          if (!prev) return prev;
          return {
            ...prev,
            status: (event.payload.status as JobStatus) || prev.status,
            percent: (event.payload.percent as number) ?? prev.percent,
            progress_message: (event.payload.message as string) ?? prev.progress_message,
          };
        });
      }
      if (event.type === "job.queue_update") {
        setLiveJob((prev) => {
          if (!prev) return prev;
          return {
            ...prev,
            queue_position: (event.payload.queue_position as number) ?? prev.queue_position,
            estimated_wait_seconds: (event.payload.estimated_wait_seconds as number) ?? prev.estimated_wait_seconds,
          };
        });
      }
      if (event.type === "job.result") {
        setLiveJob((prev) => {
          if (!prev) return prev;
          return {
            ...prev,
            status: (event.payload.status as JobStatus) || prev.status,
            result: (event.payload.result as Record<string, unknown>) || prev.result,
          };
        });
        if (event.payload.status === "succeeded") toast.success(t("jobs.jobCompleted"));
        else if (event.payload.status === "failed") toast.error(t("jobs.jobFailed"));
      }
    });

    return () => { ws.unsubscribe(`job:${jobId}`, () => {}); };
  }, [jobId, t]);

  const effectiveJob = liveJob || job;

  const handleSubmit = () => {
    if (!command.trim()) {
      setInlineError(t("jobs.enterCommand"));
      return;
    }
    setInlineError(null);
    submitJob.mutate(
      { command: command.trim(), file_ids: fileIds.length > 0 ? fileIds : undefined, skill_id: skillId || undefined, credential_ids: credentialIds.length > 0 ? credentialIds : undefined },
      {
        onSuccess: (job) => {
          setCommand(""); setFileIds([]); setSkillId(null); setCredentialIds([]);
          if (job.id) navigate(`/jobs/${job.id}`);
        },
        onError: (error: { code?: string; message?: string }) => {
          setInlineError(error.code === "QUEUE_FULL" ? t("jobs.queueFull") : (error.message || t("errors.somethingWentWrong")));
        },
      }
    );
  };

  const handleCancel = () => {
    if (effectiveJob?.id) cancelJob.mutate(effectiveJob.id);
  };

  const toggleCredential = (credId: string) => {
    setCredentialIds((prev) => prev.includes(credId) ? prev.filter((id) => id !== credId) : [...prev, credId]);
  };

  const handleOpenVNC = () => {
    if (!effectiveJob?.id) return;
    openVNC.mutate(effectiveJob.id, {
      onSuccess: (data) => {
        setVncData({ wsUrl: data.ws_url, rfbPassword: data.rfb_password, sessionId: data.session_id });
        setVncOpen(true);
      },
      onError: (error: { message?: string }) => toast.error(error.message || t("vnc.failedToOpen")),
    });
  };

  const handleSaveLogin = async (sessionId: string, label: string) => {
    try {
      await apiClient.post(`/vnc/${sessionId}/save-login`, { label });
      toast.success(t("vnc.loginSaved", { label }));
    } catch (err: unknown) {
      toast.error((err as { message?: string })?.message || t("vnc.saveLoginFailed"));
    }
  };

  const handleCloseVNC = async () => {
    if (vncData?.sessionId) {
      try { await apiClient.delete(`/vnc/${vncData.sessionId}`); } catch { /* ignore */ }
    }
    setVncOpen(false);
    setVncData(null);
  };

  const handleDownloadResult = useCallback(() => {
    if (!effectiveJob?.result) return;
    const content = formatResultContent(effectiveJob.result);
    const filename = `result-${effectiveJob.id.slice(0, 8)}.json`;
    downloadFile(content, filename);
  }, [effectiveJob?.result, effectiveJob?.id]);

  if (jobId) {
    return (
      <div className="space-y-6 p-6 max-w-2xl">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("jobs.jobDetail")}</h1>
          <p className="text-muted-foreground font-mono text-xs">{jobId}</p>
        </div>

        {jobLoading && !effectiveJob ? (
          <Card><CardContent className="space-y-4 py-6"><Skeleton className="h-6 w-32" /><Skeleton className="h-4 w-full" /><Skeleton className="h-8 w-full" /></CardContent></Card>
        ) : effectiveJob ? (
          <div className="space-y-4">
            <Card>
              <CardHeader>
                <CardTitle className="text-base">{effectiveJob.command}</CardTitle>
                <CardDescription className="text-xs font-mono">ID: {effectiveJob.id}</CardDescription>
              </CardHeader>
              <CardContent>
                <JobProgressCard
                  status={effectiveJob.status}
                  percent={effectiveJob.percent}
                  progressMessage={effectiveJob.progress_message}
                  queuePosition={effectiveJob.queue_position}
                  estimatedWaitSeconds={effectiveJob.estimated_wait_seconds}
                  startedAt={effectiveJob.started_at}
                  errorCode={effectiveJob.error_code}
                  errorMessage={effectiveJob.error_message}
                  onCancel={handleCancel}
                />
              </CardContent>
            </Card>

            {["succeeded", "failed"].includes(effectiveJob.status) && effectiveJob.result && (
              <Card>
                <CardHeader className="flex flex-row items-center justify-between">
                  <CardTitle className="text-base">{t("jobs.result")}</CardTitle>
                  <Button variant="outline" size="sm" onClick={handleDownloadResult}>
                    <Download className="mr-2 h-4 w-4" /> {t("jobs.downloadResult")}
                  </Button>
                </CardHeader>
                <CardContent>
                  <pre className="whitespace-pre-wrap rounded-md bg-muted p-4 text-sm max-h-96 overflow-auto">
                    {formatResultContent(effectiveJob.result)}
                  </pre>
                </CardContent>
              </Card>
            )}

            <div className="flex gap-2">
              {!["succeeded", "failed", "cancelled"].includes(effectiveJob.status) && (
                <Button variant="destructive" onClick={handleCancel} disabled={cancelJob.isPending}>
                  <X className="mr-2 h-4 w-4" /> {t("jobs.cancelJob")}
                </Button>
              )}
              {effectiveJob.status === "running" && (
                <Button variant="outline" onClick={handleOpenVNC} disabled={openVNC.isPending}>
                  {openVNC.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Monitor className="mr-2 h-4 w-4" />}
                  {t("jobs.openBrowser")}
                </Button>
              )}
            </div>
          </div>
        ) : (
          <Card><CardContent className="flex flex-col items-center gap-4 py-12"><p className="text-muted-foreground">{t("jobs.jobNotFound")}</p><Button variant="outline" onClick={() => navigate("/jobs")}>{t("jobs.backToCommand")}</Button></CardContent></Card>
        )}

        {vncOpen && vncData && (
          <VNCPanel open={vncOpen} onClose={handleCloseVNC} wsUrl={vncData.wsUrl} rfbPassword={vncData.rfbPassword} sessionId={vncData.sessionId} onSaveLogin={handleSaveLogin} />
        )}
      </div>
    );
  }

  return (
    <div className="space-y-6 p-6 max-w-2xl">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t("jobs.title")}</h1>
        <p className="text-muted-foreground">{t("jobs.desc")}</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("jobs.newJob")}</CardTitle>
          <CardDescription>{t("jobs.newJobDesc")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label>{t("jobs.command")}</Label>
            <Textarea placeholder={t("jobs.commandPlaceholder")} value={command} onChange={(e) => setCommand(e.target.value)} rows={3} />
          </div>

          <FileDropzone fileIds={fileIds} onFilesChange={setFileIds} disabled={submitJob.isPending} />

          {skills && skills.length > 0 && (
            <SkillSelector skills={skills} selectedSkillId={skillId} onSkillChange={setSkillId} />
          )}

          {credentials && credentials.length > 0 && (
            <div className="space-y-3">
              <div className="flex items-center gap-2">
                <Key className="h-4 w-4" />
                <Label>{t("jobs.savedLoginsDesc")}</Label>
              </div>
              <div className="flex flex-wrap gap-2">
                {credentials.map((cred) => (
                  <button key={cred.id} type="button" onClick={() => toggleCredential(cred.id)}
                    className={`inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-sm font-medium transition-colors ${credentialIds.includes(cred.id) ? "border-primary bg-primary text-primary-foreground" : "border-muted-foreground/25 bg-transparent hover:bg-accent"}`}>
                    {cred.label}
                    <span className="text-xs opacity-70">({cred.origin})</span>
                  </button>
                ))}
              </div>
            </div>
          )}

          {inlineError && (
            <div className="rounded-md bg-destructive/10 p-3 text-sm text-destructive">{inlineError}</div>
          )}

          <Button onClick={handleSubmit} disabled={submitJob.isPending || !command.trim()} className="w-full">
            {submitJob.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Send className="mr-2 h-4 w-4" />}
            {t("jobs.sendJob")}
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
