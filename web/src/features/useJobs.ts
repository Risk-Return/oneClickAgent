import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/api/client";
import type { Job, SubmitJobRequest } from "@/api/schemas";
import { toast } from "sonner";

export function useJobs(page = 0) {
  return useQuery({
    queryKey: ["jobs", page],
    queryFn: () => apiClient.get<{ items: Job[] }>(`/jobs?limit=20&cursor=${page}`),
  });
}

export function useJob(jobId: string) {
  return useQuery({
    queryKey: ["jobs", jobId],
    queryFn: () => apiClient.get<Job>(`/jobs/${jobId}`),
    enabled: !!jobId,
    refetchInterval: (query) => {
      const data = query.state.data;
      if (data && ["succeeded", "failed", "cancelled"].includes(data.status)) return false;
      return 3000;
    },
  });
}

export function useSubmitJob() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: SubmitJobRequest) => apiClient.post<Job>("/jobs", data),
    onSuccess: (job) => {
      queryClient.invalidateQueries({ queryKey: ["jobs"] });
      if (job.status === "queued") {
        toast(`Job queued at position #${job.queue_position || "?"}`);
      } else {
        toast.success("Job dispatched!");
      }
    },
    onError: (error: { code?: string; message?: string }) => {
      if (error.code === "QUEUE_FULL") {
        toast.error("Too many queued jobs — cancel one or wait.");
      } else {
        toast.error(error.message || "Failed to submit job");
      }
    },
  });
}

export function useCancelJob() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (jobId: string) => apiClient.post(`/jobs/${jobId}/cancel`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["jobs"] });
      toast.success("Job cancelled");
    },
    onError: (error: { message?: string }) => {
      toast.error(error.message || "Failed to cancel job");
    },
  });
}

export function useJobResult(jobId: string) {
  return useQuery({
    queryKey: ["jobs", jobId, "result"],
    queryFn: () => apiClient.get<unknown>(`/jobs/${jobId}/result`),
    enabled: !!jobId,
  });
}
