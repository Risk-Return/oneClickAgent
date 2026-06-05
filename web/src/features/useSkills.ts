import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/api/client";
import type { Skill, SkillRolloutEntry } from "@/api/schemas";
import { toast } from "sonner";

export function useVisibleSkills() {
  return useQuery({
    queryKey: ["skills", "visible"],
    queryFn: () => apiClient.get<Skill[]>("/skills"),
  });
}

export function useAdminSkills() {
  return useQuery({
    queryKey: ["admin", "skills"],
    queryFn: () => apiClient.getList<Skill>("/admin/skills"),
  });
}

export function useAdminSkill(skillId: string) {
  return useQuery({
    queryKey: ["admin", "skills", skillId],
    queryFn: () => apiClient.get<Skill>(`/admin/skills/${skillId}`),
    enabled: !!skillId,
  });
}

export function useCreateSkill() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: { key: string; name: string; description?: string; visibility?: string }) =>
      apiClient.post("/admin/skills", data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin", "skills"] });
      toast.success("Skill created");
    },
  });
}

export function usePublishSkillVersion() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ skillId, formData }: { skillId: string; formData: FormData }) =>
      apiClient.uploadFile(`/admin/skills/${skillId}/versions`, formData),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin", "skills"] });
      toast.success("Version published");
    },
  });
}

export function useInstallSkillFleet() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (skillId: string) => apiClient.post(`/admin/skills/${skillId}/install`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin", "skills"] });
      toast.success("Installing skill fleet-wide");
    },
  });
}

export function useDisableSkillFleet() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (skillId: string) => apiClient.post(`/admin/skills/${skillId}/disable`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin", "skills"] });
      toast.success("Skill disabled fleet-wide");
    },
  });
}

export function useEnableSkillFleet() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (skillId: string) => apiClient.post(`/admin/skills/${skillId}/enable`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin", "skills"] });
      toast.success("Skill enabled fleet-wide");
    },
  });
}

export function useDeleteSkillFleet() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (skillId: string) => apiClient.delete(`/admin/skills/${skillId}/install`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin", "skills"] });
      toast.success("Skill deleted fleet-wide");
    },
  });
}

export function useDeleteSkill() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (skillId: string) => apiClient.delete(`/admin/skills/${skillId}`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin", "skills"] });
      toast.success("Skill removed from vault");
    },
  });
}

export function useSkillRollout(skillId: string) {
  return useQuery({
    queryKey: ["admin", "skills", skillId, "rollout"],
    queryFn: () => apiClient.get<SkillRolloutEntry[]>(`/admin/skills/${skillId}/rollout`),
    enabled: !!skillId,
    refetchInterval: 3000,
  });
}

export function useRetrySkillFleet() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ skillId, deviceId, agentIds }: { skillId: string; deviceId: string; agentIds?: string[] }) =>
      apiClient.post(`/admin/skills/${skillId}/retry`, { device_id: deviceId, agent_ids: agentIds }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["admin", "skills"] });
      toast.success("Retry initiated");
    },
  });
}
