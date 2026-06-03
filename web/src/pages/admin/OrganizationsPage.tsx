import { useState } from "react";
import { apiClient } from "@/api/client";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
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
import { Building2, Plus, Trash2, Loader2 } from "lucide-react";
import type { Organization } from "@/api/schemas";

export function OrganizationsPage() {
  const queryClient = useQueryClient();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [formData, setFormData] = useState({ name: "", description: "" });
  const [creating, setCreating] = useState(false);

  const { data: orgs, isLoading } = useQuery({
    queryKey: ["admin", "orgs"],
    queryFn: () => apiClient.get<Organization[]>("/admin/orgs"),
  });

  const handleCreate = async () => {
    setCreating(true);
    try {
      await apiClient.post("/admin/orgs", {
        name: formData.name,
        description: formData.description || undefined,
      });
      queryClient.invalidateQueries({ queryKey: ["admin", "orgs"] });
      setDialogOpen(false);
      setFormData({ name: "", description: "" });
      toast.success("Organization created");
    } catch (err: unknown) {
      toast.error((err as { message?: string })?.message || "Failed to create organization");
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (orgId: string) => {
    try {
      await apiClient.delete(`/admin/orgs/${orgId}`);
      queryClient.invalidateQueries({ queryKey: ["admin", "orgs"] });
      toast.success("Organization deleted");
    } catch {
      toast.error("Failed to delete organization");
    }
  };

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Organizations</h1>
          <p className="text-muted-foreground">Manage customer groups for skill visibility grants.</p>
        </div>
        <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" /> New Organization
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Create Organization</DialogTitle>
              <DialogDescription>Create a new group to manage customers together.</DialogDescription>
            </DialogHeader>
            <div className="space-y-3">
              <div className="space-y-2">
                <Label htmlFor="org-name">Name</Label>
                <Input
                  id="org-name"
                  value={formData.name}
                  onChange={(e) => setFormData((d) => ({ ...d, name: e.target.value }))}
                  placeholder="Engineering Team"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="org-desc">Description</Label>
                <Input
                  id="org-desc"
                  value={formData.description}
                  onChange={(e) => setFormData((d) => ({ ...d, description: e.target.value }))}
                  placeholder="The engineering department"
                />
              </div>
            </div>
            <DialogFooter>
              <Button onClick={handleCreate} disabled={!formData.name || creating}>
                {creating ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
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
                <TableHead>Description</TableHead>
                <TableHead>Created</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                Array.from({ length: 3 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-40" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                  </TableRow>
                ))
              ) : orgs?.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} className="py-8 text-center text-muted-foreground">
                    <Building2 className="mx-auto h-8 w-8 mb-2" />
                    No organizations yet.
                  </TableCell>
                </TableRow>
              ) : (
                orgs?.map((org) => (
                  <TableRow key={org.id}>
                    <TableCell className="font-medium">{org.name}</TableCell>
                    <TableCell className="text-muted-foreground">{org.description || "-"}</TableCell>
                    <TableCell className="text-muted-foreground text-sm">
                      {new Date(org.created_at).toLocaleDateString()}
                    </TableCell>
                    <TableCell className="text-right">
                      <Button variant="ghost" size="icon" onClick={() => handleDelete(org.id)}>
                        <Trash2 className="h-4 w-4 text-destructive" />
                      </Button>
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
