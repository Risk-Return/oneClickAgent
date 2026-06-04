import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/api/client";
import type { Agent } from "@/api/schemas";
import { toast } from "sonner";

export function useAgents() {
  return useQuery({
    queryKey: ["agents"],
    queryFn: () => apiClient.getList<Agent>("/agents"),
  });
}

export function useAgent(agentId: string) {
  return useQuery({
    queryKey: ["agents", agentId],
    queryFn: () => apiClient.get<Agent>(`/agents/${agentId}`),
    enabled: !!agentId,
  });
}

export function useAdminAgents() {
  return useQuery({
    queryKey: ["admin", "agents"],
    queryFn: () => apiClient.getList<Agent>("/admin/agents"),
  });
}

export function useReleaseAgent() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (agentId: string) => apiClient.post(`/admin/agents/${agentId}/release`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin", "agents"] });
      toast.success("Agent released");
    },
  });
}

export function useDrainAgent() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (agentId: string) => apiClient.post(`/admin/agents/${agentId}/drain`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin", "agents"] });
      toast.success("Agent draining");
    },
  });
}
