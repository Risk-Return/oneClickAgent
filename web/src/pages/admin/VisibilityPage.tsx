import { useState } from "react";
import { useAdminSkills } from "@/features/useSkills";
import { apiClient } from "@/api/client";
import { useQueryClient } from "@tanstack/react-query";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { toast } from "sonner";
import { Eye, UserPlus, Loader2 } from "lucide-react";

export function VisibilityPage() {
  const queryClient = useQueryClient();
  const { data: skills, isLoading } = useAdminSkills();
  const [selectedSkill, setSelectedSkill] = useState<string>("");
  const [grantDialogOpen, setGrantDialogOpen] = useState(false);
  const [grantData, setGrantData] = useState({ principal_type: "user" as "user" | "org", principal_id: "" });
  const [granting, setGranting] = useState(false);

  const handleVisibilityChange = async (skillId: string, visibility: "public" | "restricted") => {
    try {
      await apiClient.patch(`/admin/skills/${skillId}/visibility`, { visibility });
      queryClient.invalidateQueries({ queryKey: ["admin", "skills"] });
      toast.success(`Skill set to ${visibility}`);
    } catch {
      toast.error("Failed to update visibility");
    }
  };

  const handleGrant = async () => {
    setGranting(true);
    try {
      await apiClient.post(`/admin/skills/${selectedSkill}/grants`, {
        principal_type: grantData.principal_type,
        principal_id: grantData.principal_id,
      });
      queryClient.invalidateQueries({ queryKey: ["admin", "skills"] });
      setGrantDialogOpen(false);
      toast.success("Grant created");
    } catch (err: unknown) {
      toast.error((err as { message?: string })?.message || "Failed to create grant");
    } finally {
      setGranting(false);
    }
  };

  return (
    <div className="space-y-6 p-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Skill Visibility</h1>
        <p className="text-muted-foreground">Control which skills customers can see and use.</p>
      </div>

      <div className="flex items-center gap-4">
        <div className="w-64">
          <Select value={selectedSkill} onValueChange={setSelectedSkill}>
            <SelectTrigger>
              <SelectValue placeholder="Select a skill..." />
            </SelectTrigger>
            <SelectContent>
              {skills?.map((skill) => (
                <SelectItem key={skill.id} value={skill.id}>
                  {skill.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        {selectedSkill && (
          <Dialog open={grantDialogOpen} onOpenChange={setGrantDialogOpen}>
            <DialogTrigger asChild>
              <Button variant="outline">
                <UserPlus className="mr-2 h-4 w-4" /> Grant Access
              </Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Grant Skill Access</DialogTitle>
                <DialogDescription>Grant visibility to a user or organization.</DialogDescription>
              </DialogHeader>
              <div className="space-y-3">
                <div className="space-y-2">
                  <Label>Principal Type</Label>
                  <Select
                    value={grantData.principal_type}
                    onValueChange={(v) => setGrantData((d) => ({ ...d, principal_type: v as "user" | "org" }))}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="user">User</SelectItem>
                      <SelectItem value="org">Organization</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label>Principal ID</Label>
                  <Input
                    value={grantData.principal_id}
                    onChange={(e) => setGrantData((d) => ({ ...d, principal_id: e.target.value }))}
                    placeholder="UUID of user or org"
                  />
                </div>
              </div>
              <DialogFooter>
                <Button onClick={handleGrant} disabled={!grantData.principal_id || granting}>
                  {granting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                  Grant
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        )}
      </div>

      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Skill</TableHead>
                <TableHead>Key</TableHead>
                <TableHead>Visibility</TableHead>
                <TableHead>Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                Array.from({ length: 3 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                  </TableRow>
                ))
              ) : skills?.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} className="py-8 text-center text-muted-foreground">
                    <Eye className="mx-auto h-8 w-8 mb-2" />
                    No skills to manage visibility for.
                  </TableCell>
                </TableRow>
              ) : (
                skills?.map((skill) => (
                  <TableRow key={skill.id}>
                    <TableCell className="font-medium">{skill.name}</TableCell>
                    <TableCell className="font-mono text-xs text-muted-foreground">{skill.key}</TableCell>
                    <TableCell>
                      <Badge variant={skill.visibility === "public" ? "success" : "secondary"}>
                        {skill.visibility}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleVisibilityChange(skill.id, "public")}
                          disabled={skill.visibility === "public"}
                        >
                          Public
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleVisibilityChange(skill.id, "restricted")}
                          disabled={skill.visibility === "restricted"}
                        >
                          Restricted
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
