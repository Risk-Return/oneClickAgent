import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { apiClient } from "@/api/client";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Download, File } from "lucide-react";
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

export function JobOutputs({ jobId, jobStatus }: JobOutputsProps) {
  const { t } = useTranslation();
  const [files, setFiles] = useState<JobOutputFile[]>([]);
  const [loading, setLoading] = useState(false);

  const fetchOutputs = useCallback(async () => {
    try {
      setLoading(true);
      const data = await apiClient.get<{ job_id: string; files: JobOutputFile[] }>(`/jobs/${jobId}/output`);
      setFiles(data?.files ?? []);
    } catch {
      // Output endpoint may not be available before job produces files
    } finally {
      setLoading(false);
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

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("jobs.outputFiles", "Output Files")}</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-2">
          {files.map((f) => (
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
  );
}
