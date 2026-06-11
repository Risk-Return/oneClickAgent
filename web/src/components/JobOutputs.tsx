import { useState, useEffect, useCallback, useRef } from "react";
import { useTranslation } from "react-i18next";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { apiClient } from "@/api/client";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Download, File, FileArchive } from "lucide-react";
import { toast } from "sonner";
import type { JobOutputFile } from "@/api/schemas";

interface JobOutputsProps {
  jobId: string;
  jobStatus: string;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
}

function isMarkdown(name: string): boolean {
  const lower = name.toLowerCase();
  return lower.endsWith(".md") || lower.endsWith(".markdown");
}

function isZip(name: string): boolean {
  return name.toLowerCase().endsWith(".zip");
}

// Summary selection: summary.md → README.md → largest markdown file.
function pickSummary(files: JobOutputFile[]): JobOutputFile | null {
  const mds = files.filter((f) => isMarkdown(f.name));
  if (mds.length === 0) return null;
  const byName = (target: string) =>
    mds.find((f) => f.name.toLowerCase().split("/").pop() === target);
  return (
    byName("summary.md") ||
    byName("readme.md") ||
    mds.reduce((a, b) => (b.size > a.size ? b : a))
  );
}

export function JobOutputs({ jobId, jobStatus }: JobOutputsProps) {
  const { t } = useTranslation();
  const [files, setFiles] = useState<JobOutputFile[]>([]);
  const [loading, setLoading] = useState(false);
  const [summaryMd, setSummaryMd] = useState<string | null>(null);
  const initialLoadRef = useRef(true);
  const summaryFileRef = useRef<string | null>(null);

  const fetchOutputs = useCallback(async () => {
    try {
      if (initialLoadRef.current) setLoading(true);
      const data = await apiClient.get<{ job_id: string; files: JobOutputFile[] }>(`/jobs/${jobId}/output`);
      setFiles(data?.files ?? []);
    } catch {
      // Output endpoint may not be available before job produces files
    } finally {
      if (initialLoadRef.current) {
        setLoading(false);
        initialLoadRef.current = false;
      }
    }
  }, [jobId]);

  useEffect(() => {
    if (jobStatus === "succeeded" || jobStatus === "failed" || jobStatus === "cancelled") {
      fetchOutputs();
      return;
    }
    if (jobStatus === "running") {
      const interval = setInterval(fetchOutputs, 5000);
      return () => clearInterval(interval);
    }
  }, [jobStatus, fetchOutputs]);

  // Fetch the markdown summary (summary.md → README.md → largest .md) for inline render.
  useEffect(() => {
    const summary = pickSummary(files);
    if (!summary) {
      summaryFileRef.current = null;
      setSummaryMd(null);
      return;
    }
    if (summaryFileRef.current === summary.file_id) return;
    summaryFileRef.current = summary.file_id;
    apiClient
      .getText(`/jobs/${jobId}/output/${summary.file_id}?inline=1`)
      .then(setSummaryMd)
      .catch(() => setSummaryMd(null));
  }, [files, jobId]);

  const handleDownload = async (fileId: string, fileName: string) => {
    try {
      const blob = await apiClient.getBlob(`/jobs/${jobId}/output/${fileId}`);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = fileName;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch (err: unknown) {
      toast.error((err as { message?: string })?.message || t("jobs.downloadFailed"));
    }
  };

  if (loading && files.length === 0) {
    return (
      <Card>
        <CardHeader><CardTitle className="text-base">{t("jobs.outputFiles", "Output Files")}</CardTitle></CardHeader>
        <CardContent><Skeleton className="h-8 w-full" /></CardContent>
      </Card>
    );
  }

  if (!loading && files.length === 0) {
    return null;
  }

  const zip = files.find((f) => isZip(f.name)) ?? null;
  const listed = files.filter((f) => f.file_id !== zip?.file_id);

  return (
    <div className="space-y-4">
      {summaryMd !== null && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("jobs.summary", "Summary")}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="prose prose-sm dark:prose-invert max-w-none">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{summaryMd}</ReactMarkdown>
            </div>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle className="text-base">{t("jobs.outputFiles", "Output Files")}</CardTitle>
          {zip && (
            <Button variant="outline" size="sm" onClick={() => handleDownload(zip.file_id, zip.name)}>
              <FileArchive className="mr-2 h-4 w-4" />
              {t("jobs.downloadAll", "Download all")}
            </Button>
          )}
        </CardHeader>
        <CardContent>
          <div className="space-y-2">
            {listed.map((f) => (
              <div key={f.file_id} className="flex items-center justify-between rounded-md border p-3">
                <div className="flex items-center gap-3 min-w-0">
                  <File className="h-4 w-4 shrink-0 text-muted-foreground" />
                  <div className="min-w-0">
                    <p className="text-sm font-medium truncate">{f.name}</p>
                    <p className="text-xs text-muted-foreground">{formatBytes(f.size)}</p>
                  </div>
                </div>
                <Button variant="outline" size="sm" onClick={() => handleDownload(f.file_id, f.name)}>
                  <Download className="mr-2 h-4 w-4" />
                  {t("common.download", "Download")}
                </Button>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
