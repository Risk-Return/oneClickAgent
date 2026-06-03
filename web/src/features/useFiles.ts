import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/api/client";
import type { FileModel } from "@/api/schemas";
import { toast } from "sonner";

export function useFiles() {
  return useQuery({
    queryKey: ["files"],
    queryFn: () => apiClient.get<FileModel[]>("/files"),
  });
}

export function useFile(fileId: string) {
  return useQuery({
    queryKey: ["files", fileId],
    queryFn: () => apiClient.get<FileModel>(`/files/${fileId}`),
    enabled: !!fileId,
  });
}

export function useDeleteFile() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (fileId: string) => apiClient.delete(`/files/${fileId}`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["files"] });
      toast.success("File deleted");
    },
  });
}
