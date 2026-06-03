import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/api/client";
import type { Job, SubmitJobRequest } from "@/api/schemas";

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
  });
}

export function useSubmitJob() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: SubmitJobRequest) => apiClient.post<Job>("/jobs", data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["jobs"] });
    },
  });
}

export function useCancelJob() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (jobId: string) => apiClient.post(`/jobs/${jobId}/cancel`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["jobs"] });
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
