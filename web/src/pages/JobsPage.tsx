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
import { JobOutputs } from "@/components/JobOutputs";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { toast } from "sonner";
import { Send, Loader2, Monitor, X, Key, Download, ChevronDown } from "lucide-react";
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
  const { jobId: routeJobId } = useParams<{ jobId: string }>();
  const navigate = useNavigate();
  const [fileIds, setFileIds] = useState<string[]>([]);
  const [skillId, setSkillId] = useState<string | null>(null);
  const [credentialIds, setCredentialIds] = useState<string[]>([]);
  const [command, setCommand] = useState("");
  const [inlineError, setInlineError] = useState<string | null>(null);
  const [vncOpen, setVncOpen] = useState(false);
  const [vncData, setVncData] = useState<{ wsUrl: string; rfbPassword: string; sessionId: string } | null>(null);
  const [loginRequired, setLoginRequired] = useState<{ origin: string; label?: string; loginKind?: string } | null>(null);

  const submitJob = useSubmitJob();
  const cancelJob = useCancelJob();
  const openVNC = useOpenVNC();
  const { data: skills } = useVisibleSkills();
  const { data: credentials } = useCredentials();

  const storedJobId = sessionStorage.getItem("iagent-active-job");
  const [activeJobId, setActiveJobId] = useState<string | null>(storedJobId);
  const [liveJob, setLiveJob] = useState<Job | null>(null);

  const detailJobId = routeJobId || activeJobId || "";
  const { data: job, isLoading: jobLoading } = useJob(detailJobId, { pollIntervalMs: 2000 });

  const updateActiveJobId = (id: string | null) => {
    setActiveJobId(id);
    if (id) {
      sessionStorage.setItem("iagent-active-job", id);
    } else {
      sessionStorage.removeItem("iagent-active-job");
    }
  };

  useEffect(() => {
    if (job && detailJobId) setLiveJob(job);
  }, [job, detailJobId]);

  useEffect(() => {
    if (!detailJobId) return;
    const ws = getWSClient();
    ws.connect();

    ws.subscribe(`job:${detailJobId}`, (event) => {
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
        setLoginRequired(null);
      }
      if (event.type === "job.login_required") {
        const payload = event.payload as Record<string, unknown>;
        setLoginRequired({
          origin: (payload.origin as string) || "",
          label: payload.label as string | undefined,
          loginKind: payload.login_kind as string | undefined,
        });
        const kind = payload.login_kind as string;
        const toastOpts: { action: { label: string; onClick: () => void }; duration?: number } = {
          action: { label: t("jobs.openBrowser"), onClick: handleOpenVNC },
          duration: Infinity,
        };
        if (kind === "qr") {
          toast(t("jobs.loginRequiredQR", { origin: payload.origin as string || "site" }), toastOpts);
        } else if (kind === "browser_ready") {
          toast(t("jobs.browserReady", "Browser is ready — open VNC to view"), toastOpts);
        } else {
          toast(t("jobs.loginRequired", { origin: payload.origin as string || "site" }), toastOpts);
        }
      }
    });

    return () => { ws.unsubscribe(`job:${detailJobId}`, () => {}); };
  }, [detailJobId, t]);

  const effectiveJob = liveJob || job;

  const handleSubmit = () => {
    if (!command.trim()) {
      setInlineError(t("jobs.enterCommand"));
      return;
    }
    setInlineError(null);

    const selectedSkill = skillId ? skills?.find((s) => s.id === skillId) : null;
    const composedCommand = selectedSkill
      ? `Please use the skill "${selectedSkill.name}" to complete the following task. Save all output files to /work/output (markdown summaries, JSON, Excel, DOCX, HTML, PDF, etc.).\n\nTask:\n${command.trim()}`
      : `Complete the following task. Save all output files to /work/output (markdown summaries, JSON, Excel, DOCX, HTML, PDF, etc.).\n\nTask:\n${command.trim()}`;

    submitJob.mutate(
      { command: composedCommand, file_ids: fileIds.length > 0 ? fileIds : undefined, skill_id: skillId || undefined, credential_ids: credentialIds.length > 0 ? credentialIds : undefined },
      {
        onSuccess: (data) => {
          const job = (data as Record<string, unknown>).job as Job;
          setCommand(""); setFileIds([]); setSkillId(null); setCredentialIds([]);
          updateActiveJobId(job.id);
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

  const handleSaveLogin = async (sessionId: string, label: string, origin: string) => {
    try {
      await apiClient.post(`/vnc/${sessionId}/save-login`, { label, origin });
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

  const commandForm = (
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

        {skillId && skills && (
          <div className="rounded-md bg-muted/50 p-3 text-xs text-muted-foreground">
            <span className="font-medium">{t("jobs.skillPrefixPreview")}:</span><br />
            Command will be prefixed with skill prompt and output instructions.<br />
            Agent saves results to <code className="text-xs bg-muted px-1 rounded">/work/output</code> (markdown, JSON, Excel, HTML, PDF, etc.)
          </div>
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
  );

  const renderJobDetail = (job: Job) => (
    <div className="space-y-4">
      {loginRequired && (
        <div className="animate-pulse rounded-lg border-2 border-amber-400 bg-amber-50 p-5 shadow-lg shadow-amber-200/50 dark:border-amber-500 dark:bg-amber-950 dark:shadow-amber-900/30">
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-3">
              <span className="flex h-8 w-8 items-center justify-center rounded-full bg-amber-500 text-white text-lg font-bold">!</span>
              <div>
                <p className="text-base font-bold text-amber-900 dark:text-amber-100">
                  {loginRequired.loginKind === "qr"
                    ? t("jobs.loginRequiredQRBanner", { origin: loginRequired.origin || "site" })
                    : loginRequired.loginKind === "browser_ready"
                      ? t("jobs.browserReadyBanner", "Browser is ready — open VNC to view the page")
                      : t("jobs.loginRequiredBanner", { origin: loginRequired.origin || "site" })}
                </p>
                <p className="text-sm text-amber-700 dark:text-amber-300">
                  {t("jobs.openBrowserToProceed", "Open the browser to continue the job")}
                </p>
              </div>
            </div>
            <div className="flex gap-2 shrink-0">
              <Button variant="outline" size="sm" onClick={() => setLoginRequired(null)}>
                <X className="mr-1 h-3 w-3" /> {t("common.dismiss")}
              </Button>
              <Button size="lg" onClick={handleOpenVNC} disabled={openVNC.isPending} className="bg-amber-600 hover:bg-amber-700 text-white font-bold shadow-lg shadow-amber-500/40 dark:bg-amber-500 dark:hover:bg-amber-600 dark:shadow-amber-400/30">
                <Monitor className="mr-2 h-5 w-5" /> {t("jobs.openBrowser")}
              </Button>
            </div>
          </div>
        </div>
      )}
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
            errorCode={job.error_code}
            errorMessage={job.error_message}
            onCancel={handleCancel}
          />
        </CardContent>
      </Card>

      <JobOutputs jobId={job.id} jobStatus={job.status} />

      {["succeeded", "failed"].includes(job.status) && job.result && (
        <Card>
          <Collapsible>
            <CardHeader className="flex flex-row items-center justify-between space-y-0">
              <CollapsibleTrigger className="flex items-center gap-2 text-base font-semibold [&[data-state=open]>svg]:rotate-180">
                <ChevronDown className="h-4 w-4 transition-transform" />
                {t("jobs.rawResult", "Raw result (JSON)")}
              </CollapsibleTrigger>
              <Button variant="outline" size="sm" onClick={handleDownloadResult}>
                <Download className="mr-2 h-4 w-4" /> {t("jobs.downloadResult")}
              </Button>
            </CardHeader>
            <CollapsibleContent>
              <CardContent>
                <pre className="whitespace-pre-wrap rounded-md bg-muted p-4 text-sm max-h-96 overflow-auto">
                  {formatResultContent(job.result)}
                </pre>
              </CardContent>
            </CollapsibleContent>
          </Collapsible>
        </Card>
      )}

      <div className="flex gap-2">
        {!["succeeded", "failed", "cancelled"].includes(job.status) && (
          <Button variant="destructive" onClick={handleCancel} disabled={cancelJob.isPending}>
            <X className="mr-2 h-4 w-4" /> {t("jobs.cancelJob")}
          </Button>
        )}
        {job.status === "running" && (
          <Button variant="outline" onClick={handleOpenVNC} disabled={openVNC.isPending}>
            {openVNC.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Monitor className="mr-2 h-4 w-4" />}
            {t("jobs.openBrowser")}
          </Button>
        )}
      </div>
    </div>
  );

  if (routeJobId) {
    return (
      <div className="space-y-6 p-6 max-w-2xl">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("jobs.jobDetail")}</h1>
          <p className="text-muted-foreground font-mono text-xs">{routeJobId}</p>
        </div>

        {jobLoading && !effectiveJob ? (
          <Card><CardContent className="space-y-4 py-6"><Skeleton className="h-6 w-32" /><Skeleton className="h-4 w-full" /><Skeleton className="h-8 w-full" /></CardContent></Card>
        ) : effectiveJob ? (
          renderJobDetail(effectiveJob)
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
    <div className="flex h-full">
      <div className="w-1/2 min-w-0 overflow-auto border-r p-6">
        <div className="space-y-6 max-w-xl">
          <div>
            <h1 className="text-2xl font-bold tracking-tight">{t("jobs.title")}</h1>
            <p className="text-muted-foreground">{t("jobs.desc")}</p>
          </div>
          {commandForm}
        </div>
      </div>

      <div className="w-1/2 min-w-0 overflow-auto p-6">
        {activeJobId ? (
          <div className="space-y-6 max-w-xl">
            <div>
              <h2 className="text-xl font-semibold tracking-tight">{t("jobs.jobDetail")}</h2>
              {effectiveJob && (
                <p className="text-muted-foreground font-mono text-xs">{effectiveJob.id}</p>
              )}
            </div>

            {jobLoading && !effectiveJob ? (
              <Card><CardContent className="space-y-4 py-6"><Skeleton className="h-6 w-32" /><Skeleton className="h-4 w-full" /><Skeleton className="h-8 w-full" /></CardContent></Card>
            ) : effectiveJob ? (
              renderJobDetail(effectiveJob)
            ) : (
              <Card><CardContent className="flex flex-col items-center gap-4 py-12"><p className="text-muted-foreground">{t("jobs.jobNotFound")}</p></CardContent></Card>
            )}
          </div>
        ) : (
          <div className="flex h-full items-center justify-center">
            <div className="text-center space-y-3">
              <p className="text-muted-foreground text-sm">{t("jobs.desc")}</p>
            </div>
          </div>
        )}
      </div>

      {vncOpen && vncData && (
        <VNCPanel open={vncOpen} onClose={handleCloseVNC} wsUrl={vncData.wsUrl} rfbPassword={vncData.rfbPassword} sessionId={vncData.sessionId} onSaveLogin={handleSaveLogin} />
      )}
    </div>
  );
}
