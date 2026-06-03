import { useState } from "react";
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
import { Server, Plus, Trash2, Key } from "lucide-react";
import type { Device } from "@/api/schemas";

export function DeviceFleetPage() {
  const queryClient = useQueryClient();
  const [newDeviceData, setNewDeviceData] = useState({ name: "", description: "" });
  const [dialogOpen, setDialogOpen] = useState(false);

  const { data: devices, isLoading } = useQuery({
    queryKey: ["admin", "devices"],
    queryFn: () => apiClient.get<Device[]>("/devices"),
  });

  const handleCreateDevice = async () => {
    try {
      const result = await apiClient.post<{ id: string; enrollment_code: string }>("/devices", {
        name: newDeviceData.name,
        description: newDeviceData.description || undefined,
      });
      queryClient.invalidateQueries({ queryKey: ["admin", "devices"] });
      setDialogOpen(false);
      setNewDeviceData({ name: "", description: "" });
      toast.success(`Device created. Enrollment code: ${result.enrollment_code}`);
    } catch (err: unknown) {
      toast.error((err as { message?: string })?.message || "Failed to create device");
    }
  };

  const handleRotateToken = async (deviceId: string) => {
    try {
      await apiClient.post(`/devices/${deviceId}/rotate-token`);
      queryClient.invalidateQueries({ queryKey: ["admin", "devices"] });
      toast.success("Token rotated");
    } catch {
      toast.error("Failed to rotate token");
    }
  };

  const handleDeleteDevice = async (deviceId: string) => {
    try {
      await apiClient.delete(`/devices/${deviceId}`);
      queryClient.invalidateQueries({ queryKey: ["admin", "devices"] });
      toast.success("Device decommissioned");
    } catch {
      toast.error("Failed to decommission device");
    }
  };

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Device Fleet</h1>
          <p className="text-muted-foreground">Manage your local devices and their agent pools.</p>
        </div>
        <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" /> Add Device
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Add Device</DialogTitle>
              <DialogDescription>Create a new device. An enrollment code will be generated.</DialogDescription>
            </DialogHeader>
            <div className="space-y-3">
              <div className="space-y-2">
                <Label htmlFor="device-name">Name</Label>
                <Input
                  id="device-name"
                  value={newDeviceData.name}
                  onChange={(e) => setNewDeviceData((d) => ({ ...d, name: e.target.value }))}
                  placeholder="home-server"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="device-desc">Description (optional)</Label>
                <Input
                  id="device-desc"
                  value={newDeviceData.description}
                  onChange={(e) => setNewDeviceData((d) => ({ ...d, description: e.target.value }))}
                  placeholder="Living room Mac Mini"
                />
              </div>
            </div>
            <DialogFooter>
              <Button onClick={handleCreateDevice} disabled={!newDeviceData.name}>
                Create
              </Button>
            </DialogFooter>
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
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                  </TableRow>
                ))
              ) : devices?.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={5} className="py-8 text-center text-muted-foreground">
                    <Server className="mx-auto h-8 w-8 mb-2" />
                    No devices yet.
                  </TableCell>
                </TableRow>
              ) : (
                devices?.map((device) => (
                  <TableRow key={device.id}>
                    <TableCell className="font-medium">{device.name}</TableCell>
                    <TableCell>
                      <AgentStatusBadge status={device.status} />
                    </TableCell>
                    <TableCell className="text-muted-foreground">{device.platform || "-"}</TableCell>
                    <TableCell className="text-muted-foreground text-sm">
                      {device.last_seen_at ? new Date(device.last_seen_at).toLocaleDateString() : "-"}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Button variant="ghost" size="icon" onClick={() => handleRotateToken(device.id)}>
                          <Key className="h-4 w-4" />
                        </Button>
                        <Button variant="ghost" size="icon" onClick={() => handleDeleteDevice(device.id)}>
                          <Trash2 className="h-4 w-4 text-destructive" />
                        </Button>
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
