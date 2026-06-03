import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { Card } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { Button } from "@/components/ui/button";
import { apiClient } from "@/api/client";
import { Upload, X, File } from "lucide-react";

interface UploadingFile {
  name: string;
  size: number;
  progress: number;
  id?: string;
}

interface FileDropzoneProps {
  fileIds: string[];
  onFilesChange: (ids: string[]) => void;
  disabled?: boolean;
}

export function FileDropzone({ fileIds, onFilesChange, disabled }: FileDropzoneProps) {
  const { t } = useTranslation();
  const [uploading, setUploading] = useState<UploadingFile[]>([]);
  const [dragOver, setDragOver] = useState(false);

  const uploadFile = useCallback(async (file: File) => {
    const entry: UploadingFile = { name: file.name, size: file.size, progress: 0 };
    setUploading((prev) => [...prev, entry]);

    const formData = new FormData();
    formData.append("file", file);

    const result = await apiClient.uploadFile("/files", formData) as { id: string };
    setUploading((prev) => prev.filter((f) => f.name !== file.name));

    if (result?.id) {
      onFilesChange([...fileIds, result.id]);
    }
  }, [fileIds, onFilesChange]);

  const handleDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    setDragOver(false);
    const files = Array.from(e.dataTransfer.files);
    files.forEach(uploadFile);
  }, [uploadFile]);

  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault();
    setDragOver(true);
  };

  return (
    <div className="space-y-3">
      <Card
        className={`flex flex-col items-center justify-center gap-2 border-2 border-dashed p-6 transition-colors ${
          dragOver ? "border-primary bg-primary/5" : "border-muted-foreground/25"
        } ${disabled ? "pointer-events-none opacity-50" : "cursor-pointer"}`}
        onDrop={handleDrop}
        onDragOver={handleDragOver}
        onDragLeave={() => setDragOver(false)}
      >
        <Upload className="h-8 w-8 text-muted-foreground" />
        <p className="text-sm text-muted-foreground">{t("fileDropzone.dropHere")}</p>
        <input
          type="file"
          className="hidden"
          id="file-upload"
          multiple
          onChange={(e) => {
            const files = Array.from(e.target.files || []);
            files.forEach(uploadFile);
            e.target.value = "";
          }}
        />
        <Button variant="outline" size="sm" asChild>
          <label htmlFor="file-upload" className="cursor-pointer">{t("fileDropzone.chooseFiles")}</label>
        </Button>
      </Card>

      {uploading.map((f) => (
        <div key={f.name} className="space-y-1">
          <div className="flex items-center gap-2 text-sm">
            <File className="h-4 w-4 text-muted-foreground" />
            <span className="truncate flex-1">{f.name}</span>
            <span className="text-muted-foreground">{t("fileDropzone.uploading")}</span>
          </div>
          <Progress value={f.progress} className="h-1" />
        </div>
      ))}

      {fileIds.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {fileIds.map((id) => (
            <span
              key={id}
              className="inline-flex items-center gap-1 rounded-full border bg-accent px-2.5 py-1 text-xs"
            >
              <File className="h-3 w-3" />
              {id.slice(0, 8)}...
              <button
                type="button"
                onClick={() => onFilesChange(fileIds.filter((fid) => fid !== id))}
                className="ml-1 rounded-full hover:text-destructive"
              >
                <X className="h-3 w-3" />
              </button>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
