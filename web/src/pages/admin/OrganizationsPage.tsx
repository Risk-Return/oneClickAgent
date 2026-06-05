import { useState, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { apiClient } from "@/api/client";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter,
  DialogHeader, DialogTitle, DialogTrigger,
} from "@/components/ui/dialog";
import { toast } from "sonner";
import { Building2, Plus, Trash2, Loader2, Users, UserPlus, UserX, Copy, Check } from "lucide-react";

function UUIDCell({ uuid }: { uuid: string }) {
  const [expanded, setExpanded] = useState(false);
  const [copied, setCopied] = useState(false);
  const copy = useCallback((e: React.MouseEvent) => {
    e.stopPropagation();
    navigator.clipboard.writeText(uuid);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  }, [uuid]);
  return (
    <button type="button"
      className="font-mono text-xs text-left hover:text-foreground text-muted-foreground flex items-center gap-1 group"
      onClick={() => setExpanded(!expanded)}
      title={expanded ? "Click to collapse" : "Click to reveal full UUID"}
    >
      <span>{expanded ? uuid : uuid.slice(0, 8) + "..."}</span>
      <button type="button" onClick={copy} className="opacity-0 group-hover:opacity-100 transition-opacity" title="Copy UUID">
        {copied ? <Check className="h-3 w-3 text-green-500" /> : <Copy className="h-3 w-3" />}
      </button>
    </button>
  );
}
import type { Organization } from "@/api/schemas";

interface Member {
  id: string;
  username: string;
  email: string;
}

export function OrganizationsPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [formData, setFormData] = useState({ name: "", description: "" });
  const [creating, setCreating] = useState(false);
  const [selectedOrgId, setSelectedOrgId] = useState<string | null>(null);
  const [addMemberId, setAddMemberId] = useState("");

  const { data: orgs, isLoading } = useQuery({
    queryKey: ["admin", "orgs"],
    queryFn: () => apiClient.getList<Organization>("/admin/orgs"),
  });

  const { data: members, isLoading: membersLoading, refetch: refetchMembers } = useQuery({
    queryKey: ["admin", "orgs", selectedOrgId, "members"],
    queryFn: () => apiClient.get<Member[]>(`/admin/orgs/${selectedOrgId}/members`),
    enabled: !!selectedOrgId,
    staleTime: 0,
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
      toast.success(t("organizations.orgCreated"));
    } catch (err: unknown) {
      toast.error((err as { message?: string })?.message || t("organizations.createFailed"));
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (orgId: string) => {
    try {
      await apiClient.delete(`/admin/orgs/${orgId}`);
      queryClient.invalidateQueries({ queryKey: ["admin", "orgs"] });
      if (selectedOrgId === orgId) setSelectedOrgId(null);
      toast.success(t("organizations.orgDeleted"));
    } catch {
      toast.error(t("organizations.deleteFailed"));
    }
  };

  const handleAddMember = async () => {
    if (!selectedOrgId || !addMemberId) return;
    try {
      await apiClient.post(`/admin/orgs/${selectedOrgId}/members`, { user_id: addMemberId });
      queryClient.invalidateQueries({ queryKey: ["admin", "orgs", selectedOrgId, "members"] });
      refetchMembers();
      setAddMemberId("");
      toast.success(t("organizations.memberAdded"));
    } catch (err: unknown) {
      toast.error((err as { message?: string })?.message || t("organizations.addMemberFailed"));
    }
  };

  const handleRemoveMember = async (userId: string) => {
    if (!selectedOrgId) return;
    try {
      await apiClient.delete(`/admin/orgs/${selectedOrgId}/members/${userId}`);
      refetchMembers();
      toast.success(t("organizations.memberRemoved"));
    } catch {
      toast.error(t("organizations.removeMemberFailed"));
    }
  };

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("organizations.title")}</h1>
          <p className="text-muted-foreground">{t("organizations.desc")}</p>
        </div>
        <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" /> {t("organizations.newOrg")}
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t("organizations.createTitle")}</DialogTitle>
              <DialogDescription>{t("organizations.createDesc")}</DialogDescription>
            </DialogHeader>
            <div className="space-y-3">
              <div className="space-y-2">
                <Label htmlFor="org-name">{t("common.name")}</Label>
                <Input id="org-name" value={formData.name} onChange={(e) => setFormData((d) => ({ ...d, name: e.target.value }))} placeholder={t("organizations.namePlaceholder")} />
              </div>
              <div className="space-y-2">
                <Label htmlFor="org-desc">{t("common.description")}</Label>
                <Input id="org-desc" value={formData.description} onChange={(e) => setFormData((d) => ({ ...d, description: e.target.value }))} placeholder={t("organizations.descPlaceholder")} />
              </div>
            </div>
            <DialogFooter>
              <Button onClick={handleCreate} disabled={!formData.name || creating}>
                {creating ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                {t("common.create")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      <div className="grid gap-6 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Organizations</CardTitle>
          </CardHeader>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
              <TableRow>
                <TableHead>UUID</TableHead>
                <TableHead>Name</TableHead>
                <TableHead>Created</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
              </TableHeader>
              <TableBody>
                {isLoading ? (
                  Array.from({ length: 3 }).map((_, i) => (
                    <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    </TableRow>
                  ))
                ) : orgs?.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={4} className="py-8 text-center text-muted-foreground">
                    <Building2 className="mx-auto h-8 w-8 mb-2" />
                    {t("organizations.noOrgs")}
                    </TableCell>
                  </TableRow>
                ) : (
                  orgs?.map((org) => (
                    <TableRow
                      key={org.id}
                      className="cursor-pointer hover:bg-muted/50"
                      onClick={() => setSelectedOrgId(org.id)}
                    >
                      <TableCell><UUIDCell uuid={org.id} /></TableCell>
                      <TableCell className="font-medium">{org.name}</TableCell>
                      <TableCell className="text-muted-foreground text-sm">
                        {new Date(org.created_at).toLocaleDateString()}
                      </TableCell>
                      <TableCell className="text-right">
                        <Button variant="ghost" size="icon" onClick={(e) => { e.stopPropagation(); handleDelete(org.id); }}>
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

        {selectedOrgId && (
          <Card>
            <CardHeader className="flex flex-row items-center justify-between">
              <CardTitle className="text-base">{t("organizations.members")}</CardTitle>
              <div className="flex items-center gap-2">
                <Input
                  placeholder={t("organizations.userUuid")}
                  value={addMemberId}
                  onChange={(e) => setAddMemberId(e.target.value)}
                  className="h-8 w-40 text-xs"
                  onKeyDown={(e) => e.key === "Enter" && handleAddMember()}
                />
                <Button size="sm" variant="outline" onClick={handleAddMember} disabled={!addMemberId}>
                  <UserPlus className="mr-1 h-3 w-3" /> {t("organizations.addMember")}
                </Button>
              </div>
            </CardHeader>
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Username</TableHead>
                    <TableHead>Email</TableHead>
                    <TableHead className="text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {membersLoading ? (
                    <TableRow><TableCell colSpan={3}><Skeleton className="h-4 w-full" /></TableCell></TableRow>
                  ) : !members || members.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={3} className="py-4 text-center text-muted-foreground text-sm">
                      <Users className="mx-auto h-6 w-6 mb-1" />
                      {t("organizations.noMembers")}
                      </TableCell>
                    </TableRow>
                  ) : (
                    members.map((member) => (
                      <TableRow key={member.id}>
                        <TableCell className="font-medium">{member.username}</TableCell>
                        <TableCell className="text-muted-foreground text-sm">{member.email}</TableCell>
                        <TableCell className="text-right">
                          <Button variant="ghost" size="icon" onClick={() => handleRemoveMember(member.id)}>
                            <UserX className="h-4 w-4 text-destructive" />
                          </Button>
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  );
}
