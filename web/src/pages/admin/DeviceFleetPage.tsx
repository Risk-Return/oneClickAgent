import { useState } from "react";
import { useTranslation } from "react-i18next";
import { apiClient } from "@/api/client";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { AgentStatusBadge } from "@/components/AgentStatusBadge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { toast } from "sonner";
import { Server, Plus, Trash2, Key, Pencil, Layers, Copy, Check } from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import type { Device } from "@/api/schemas";

export function DeviceFleetPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [enrollmentResult, setEnrollmentResult] = useState<{ code: string; copied: boolean } | null>(null);
  const [newDeviceData, setNewDeviceData] = useState({ name: "", description: "" });
  const [renameId, setRenameId] = useState<string | null>(null);
  const [renameName, setRenameName] = useState("");
  const [poolSizeId, setPoolSizeId] = useState<string | null>(null);
  const [poolSize, setPoolSize] = useState(4);

  const { data: devices, isLoading } = useQuery({
    queryKey: ["admin", "devices"],
    queryFn: () => apiClient.getList<Device>("/devices"),
  });

  const handleCreateDevice = async () => {
    try {
      const result = await apiClient.post<{ id: string; enrollment_code: string }>("/devices", {
        name: newDeviceData.name,
        description: newDeviceData.description || undefined,
      });
      queryClient.invalidateQueries({ queryKey: ["admin", "devices"] });
      setNewDeviceData({ name: "", description: "" });
      setEnrollmentResult({ code: result.enrollment_code, copied: false });
    } catch (err: unknown) {
      toast.error((err as { message?: string })?.message || t("deviceFleet.createFailed"));
    }
  };

  const handleCopyCode = () => {
    if (enrollmentResult) {
      navigator.clipboard.writeText(enrollmentResult.code);
      setEnrollmentResult({ ...enrollmentResult, copied: true });
      setTimeout(() => setEnrollmentResult(null), 5000);
    }
  };

  const handleCloseDialog = () => {
    setDialogOpen(false);
    setEnrollmentResult(null);
  };

  const handleRename = async (deviceId: string) => {
    try {
      await apiClient.patch(`/devices/${deviceId}`, { name: renameName });
      queryClient.invalidateQueries({ queryKey: ["admin", "devices"] });
      setRenameId(null);
      toast.success(t("deviceFleet.deviceRenamed"));
    } catch (err: unknown) {
      toast.error((err as { message?: string })?.message || t("deviceFleet.renameFailed"));
    }
  };

  const handlePoolSize = async (deviceId: string) => {
    try {
      await apiClient.post(`/admin/devices/${deviceId}/pool`, { size: poolSize });
      queryClient.invalidateQueries({ queryKey: ["admin", "devices"] });
      setPoolSizeId(null);
      toast.success(t("deviceFleet.poolSizeSet", { size: poolSize }));
    } catch (err: unknown) {
      toast.error((err as { message?: string })?.message || t("deviceFleet.poolSizeFailed"));
    }
  };

  const handleRotateToken = async (deviceId: string) => {
    try {
      await apiClient.post(`/devices/${deviceId}/rotate-token`);
      queryClient.invalidateQueries({ queryKey: ["admin", "devices"] });
      toast.success(t("deviceFleet.tokenRotated"));
    } catch {
      toast.error(t("deviceFleet.tokenRotateFailed"));
    }
  };

  const handleDeleteDevice = async (deviceId: string) => {
    try {
      await apiClient.delete(`/devices/${deviceId}`);
      queryClient.invalidateQueries({ queryKey: ["admin", "devices"] });
      toast.success(t("deviceFleet.deviceDecommissioned"));
    } catch {
      toast.error(t("deviceFleet.decommissionFailed"));
    }
  };

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("deviceFleet.title")}</h1>
          <p className="text-muted-foreground">{t("deviceFleet.desc")}</p>
        </div>
        <Dialog open={dialogOpen} onOpenChange={(open) => { open ? setDialogOpen(true) : handleCloseDialog(); }}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" /> {t("deviceFleet.addDevice")}
            </Button>
          </DialogTrigger>
          <DialogContent className="max-w-md">
            <DialogHeader>
              <DialogTitle>{t("deviceFleet.addDeviceTitle")}</DialogTitle>
              <DialogDescription>{t("deviceFleet.addDeviceDesc")}</DialogDescription>
            </DialogHeader>

            {enrollmentResult ? (
              <div className="space-y-4">
                <div className="rounded-md bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 p-4">
                  <p className="text-sm font-medium text-emerald-700 dark:text-emerald-300">{t("deviceFleet.deviceCreated")}</p>
                  <p className="text-xs text-emerald-600 dark:text-emerald-400 mt-1">{t("deviceFleet.copyCode")}</p>
                  <div className="mt-2 flex items-center gap-2">
                    <code className="flex-1 rounded bg-emerald-100 dark:bg-emerald-900/40 px-3 py-2 text-sm font-mono break-all">
                      {enrollmentResult.code}
                    </code>
                    <Button size="icon" variant="outline" onClick={handleCopyCode}>
                      {enrollmentResult.copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                    </Button>
                  </div>
                </div>
                <div className="space-y-2 text-sm text-muted-foreground">
                  <p className="font-medium">{t("deviceFleet.setupInstructions")}</p>
                  <div className="space-y-2 text-xs">
                    <div>
                      <strong>{t("deviceFleet.setupWindows")}</strong> Install Docker Desktop, then run:
                      <code className="block mt-1 rounded bg-muted px-2 py-1">iagent-device enroll --code {enrollmentResult.code}</code>
                    </div>
                    <div>
                      <strong>{t("deviceFleet.setupMacos")}</strong> Install Docker Desktop, then run:
                      <code className="block mt-1 rounded bg-muted px-2 py-1">iagent-device enroll --code {enrollmentResult.code}</code>
                    </div>
                    <div>
                      <strong>{t("deviceFleet.setupLinux")}</strong> Install Docker Engine, then run:
                      <code className="block mt-1 rounded bg-muted px-2 py-1">iagent-device enroll --code {enrollmentResult.code}</code>
                    </div>
                  </div>
                </div>
                <DialogFooter>
                  <Button onClick={handleCloseDialog}>{t("common.done")}</Button>
                </DialogFooter>
              </div>
            ) : (
              <>
                <div className="space-y-3">
                  <div className="space-y-2">
                    <Label htmlFor="device-name">{t("deviceFleet.deviceName")}</Label>
                    <Input
                      id="device-name"
                      value={newDeviceData.name}
                      onChange={(e) => setNewDeviceData((d) => ({ ...d, name: e.target.value }))}
                      placeholder={t("deviceFleet.deviceNamePlaceholder")}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="device-desc">{t("deviceFleet.deviceDesc")}</Label>
                    <Input
                      id="device-desc"
                      value={newDeviceData.description}
                      onChange={(e) => setNewDeviceData((d) => ({ ...d, description: e.target.value }))}
                      placeholder={t("deviceFleet.deviceDescPlaceholder")}
                    />
                  </div>
                </div>
                <DialogFooter>
                  <Button onClick={handleCreateDevice} disabled={!newDeviceData.name}>
                    {t("common.create")}
                  </Button>
                </DialogFooter>
              </>
            )}
          </DialogContent>
        </Dialog>
      </div>

      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Platform</TableHead>
                <TableHead>Pool Size</TableHead>
                <TableHead>Last Seen</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                Array.from({ length: 3 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-12" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                  </TableRow>
                ))
              ) : devices?.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="py-8 text-center text-muted-foreground">
                    <Server className="mx-auto h-8 w-8 mb-2" />
                    {t("deviceFleet.noDevices")}
                  </TableCell>
                </TableRow>
              ) : (
                devices?.map((device) => (
                  <TableRow key={device.id}>
                    <TableCell className="font-medium">
                      {renameId === device.id ? (
                        <div className="flex items-center gap-1">
                          <Input
                            value={renameName}
                            onChange={(e) => setRenameName(e.target.value)}
                            className="h-8 w-32"
                            autoFocus
                            onKeyDown={(e) => e.key === "Enter" && handleRename(device.id)}
                          />
                          <Button variant="ghost" size="icon" onClick={() => handleRename(device.id)} className="h-8 w-8">
                            <Check className="h-4 w-4" />
                          </Button>
                        </div>
                      ) : (
                        device.name
                      )}
                    </TableCell>
                    <TableCell>
                      <AgentStatusBadge status={device.status} />
                    </TableCell>
                    <TableCell className="text-muted-foreground">{device.platform || "-"}</TableCell>
                    <TableCell>
                      {poolSizeId === device.id ? (
                        <div className="flex items-center gap-1">
                          <Input
                            type="number"
                            min={1}
                            max={20}
                            value={poolSize}
                            onChange={(e) => setPoolSize(Number(e.target.value))}
                            className="h-8 w-16"
                            onKeyDown={(e) => e.key === "Enter" && handlePoolSize(device.id)}
                          />
                          <Button variant="ghost" size="icon" onClick={() => handlePoolSize(device.id)} className="h-8 w-8">
                            <Check className="h-4 w-4" />
                          </Button>
                        </div>
                      ) : (
                        <Button variant="ghost" size="sm" className="text-muted-foreground" onClick={() => { setPoolSizeId(device.id); setPoolSize(4); }}>
                          <Layers className="mr-1 h-3 w-3" /> {t("deviceFleet.configure")}
                        </Button>
                      )}
                    </TableCell>
                    <TableCell className="text-muted-foreground text-sm">
                      {device.last_seen_at ? new Date(device.last_seen_at).toLocaleDateString() : "-"}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button variant="ghost" size="icon" onClick={() => { setRenameId(device.id); setRenameName(device.name); }}>
                              <Pencil className="h-4 w-4" />
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent side="top" align="start" className="text-left">
                            Rename device
                          </TooltipContent>
                        </Tooltip>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button variant="ghost" size="icon" onClick={() => handleRotateToken(device.id)}>
                              <Key className="h-4 w-4" />
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent side="top" align="start" className="text-left">
                            Rotate device token.<br />Revokes old token, issues new one.
                          </TooltipContent>
                        </Tooltip>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button variant="ghost" size="icon" onClick={() => handleDeleteDevice(device.id)}>
                              <Trash2 className="h-4 w-4 text-destructive" />
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent side="top" align="start" className="text-left">
                            Decommission device.<br />Removes from fleet. Must be offline.
                          </TooltipContent>
                        </Tooltip>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}
