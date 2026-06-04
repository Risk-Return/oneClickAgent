import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/api/client";
import type { BrowserCredential } from "@/api/schemas";
import { toast } from "sonner";

export function useCredentials() {
  return useQuery({
    queryKey: ["credentials"],
    queryFn: () => apiClient.getList<BrowserCredential>("/credentials"),
  });
}

export function useCredential(credentialId: string) {
  return useQuery({
    queryKey: ["credentials", credentialId],
    queryFn: () => apiClient.get<BrowserCredential>(`/credentials/${credentialId}`),
    enabled: !!credentialId,
  });
}

export function useUpdateCredential() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, label }: { id: string; label: string }) =>
      apiClient.patch(`/credentials/${id}`, { label }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["credentials"] });
      toast.success("Credential updated");
    },
    onError: (error: { message?: string }) => {
      toast.error(error.message || "Failed to update credential");
    },
  });
}

export function useDeleteCredential() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (credentialId: string) => apiClient.delete(`/credentials/${credentialId}`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["credentials"] });
      toast.success("Credential deleted");
    },
    onError: (error: { message?: string }) => {
      toast.error(error.message || "Failed to delete credential");
    },
  });
}

export function useOpenVNC() {
  return useMutation({
    mutationFn: (jobId: string) =>
      apiClient.post<{ session_id: string; ws_url: string; rfb_password: string; ttl_s: number }>(`/jobs/${jobId}/vnc`),
    onError: (error: { message?: string }) =>
      toast.error(error.message || "Failed to open browser"),
  });
}
