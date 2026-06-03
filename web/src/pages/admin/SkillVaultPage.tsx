import { useState } from "react";
import { useAdminSkills, useCreateSkill, useInstallSkillFleet } from "@/features/useSkills";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
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
import { Archive, Plus, Download, Loader2 } from "lucide-react";

export function SkillVaultPage() {
  const { data: skills, isLoading } = useAdminSkills();
  const createSkill = useCreateSkill();
  const installSkill = useInstallSkillFleet();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [formData, setFormData] = useState({ key: "", name: "", description: "", visibility: "restricted" });

  const handleCreate = () => {
    createSkill.mutate(
      {
        key: formData.key,
        name: formData.name,
        description: formData.description || undefined,
        visibility: formData.visibility,
      },
      {
        onSuccess: () => {
          setDialogOpen(false);
          setFormData({ key: "", name: "", description: "", visibility: "restricted" });
        },
      }
    );
  };

  const handleInstall = (skillId: string) => {
    installSkill.mutate(skillId);
  };

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Skill Vault</h1>
          <p className="text-muted-foreground">Central catalog of skills for the agent fleet.</p>
        </div>
        <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" /> New Skill
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Create Skill Entry</DialogTitle>
              <DialogDescription>Add a new skill to the vault catalog.</DialogDescription>
            </DialogHeader>
            <div className="space-y-3">
              <div className="space-y-2">
                <Label htmlFor="skill-key">Key (slug)</Label>
                <Input
                  id="skill-key"
                  value={formData.key}
                  onChange={(e) => setFormData((d) => ({ ...d, key: e.target.value }))}
                  placeholder="pdf-extract"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="skill-name">Display Name</Label>
                <Input
                  id="skill-name"
                  value={formData.name}
                  onChange={(e) => setFormData((d) => ({ ...d, name: e.target.value }))}
                  placeholder="PDF Extraction"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="skill-desc">Description</Label>
                <Input
                  id="skill-desc"
                  value={formData.description}
                  onChange={(e) => setFormData((d) => ({ ...d, description: e.target.value }))}
                  placeholder="Extracts text from PDF documents"
                />
              </div>
            </div>
            <DialogFooter>
              <Button onClick={handleCreate} disabled={!formData.key || !formData.name || createSkill.isPending}>
                {createSkill.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
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
                <TableHead>Key</TableHead>
                <TableHead>Name</TableHead>
                <TableHead>Visibility</TableHead>
                <TableHead>Version</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                Array.from({ length: 3 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-12" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                  </TableRow>
                ))
              ) : skills?.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="py-8 text-center text-muted-foreground">
                    <Archive className="mx-auto h-8 w-8 mb-2" />
                    No skills in vault.
                  </TableCell>
                </TableRow>
              ) : (
                skills?.map((skill) => (
                  <TableRow key={skill.id}>
                    <TableCell className="font-mono text-xs">{skill.key}</TableCell>
                    <TableCell className="font-medium">{skill.name}</TableCell>
                    <TableCell>
                      <Badge variant={skill.visibility === "public" ? "success" : "secondary"}>
                        {skill.visibility}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-muted-foreground">{skill.latest_version || "-"}</TableCell>
                    <TableCell>
                      <Badge variant={skill.status === "active" ? "success" : "secondary"}>{skill.status}</Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      <Button variant="ghost" size="sm" onClick={() => handleInstall(skill.id)} disabled={installSkill.isPending}>
                        <Download className="mr-1 h-3 w-3" /> Install fleet
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
