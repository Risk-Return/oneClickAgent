import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useAdminSkills, useCreateSkill, useInstallSkillFleet, usePublishSkillVersion } from "@/features/useSkills";
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
import { Archive, Plus, Download, Loader2, Upload, HelpCircle } from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";

export function SkillVaultPage() {
  const { t } = useTranslation();
  const { data: skills, isLoading } = useAdminSkills();
  const createSkill = useCreateSkill();
  const installSkill = useInstallSkillFleet();
  const publishVersion = usePublishSkillVersion();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [formData, setFormData] = useState({ key: "", name: "", description: "" });
  const [publishSkillId, setPublishSkillId] = useState<string | null>(null);
  const [publishVersion_str, setPublishVersionStr] = useState("");
  const [publishFile, setPublishFile] = useState<File | null>(null);

  const handleCreate = () => {
    createSkill.mutate(
      { key: formData.key, name: formData.name, description: formData.description || undefined },
      {
        onSuccess: () => {
          setDialogOpen(false);
          setFormData({ key: "", name: "", description: "" });
        },
      }
    );
  };

  const handleInstall = (skillId: string) => {
    installSkill.mutate(skillId);
  };

  const handlePublish = () => {
    if (!publishSkillId || !publishVersion_str || !publishFile) return;
    const fd = new FormData();
    fd.append("version", publishVersion_str);
    fd.append("manifest", JSON.stringify({
      name: skills?.find((s) => s.id === publishSkillId)?.key || publishSkillId,
      version: publishVersion_str,
      entrypoint: publishFile.name,
      type: "claude-code",
    }));
    fd.append("artifact", publishFile);
    publishVersion.mutate(
      { skillId: publishSkillId, formData: fd },
      {
        onSuccess: () => {
          setPublishSkillId(null);
          setPublishVersionStr("");
          setPublishFile(null);
        },
      }
    );
  };

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("skillVault.title")}</h1>
          <p className="text-muted-foreground">{t("skillVault.desc")}</p>
        </div>
        <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" /> {t("skillVault.newSkill")}
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t("skillVault.createTitle")}</DialogTitle>
              <DialogDescription>{t("skillVault.createDesc")}</DialogDescription>
            </DialogHeader>
            <div className="space-y-3">
              <div className="space-y-2">
                <Label htmlFor="skill-key">{t("skillVault.key")}</Label>
                <Input
                  id="skill-key"
                  value={formData.key}
                  onChange={(e) => setFormData((d) => ({ ...d, key: e.target.value }))}
                  placeholder={t("skillVault.keyPlaceholder")}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="skill-name">{t("skillVault.displayName")}</Label>
                <Input
                  id="skill-name"
                  value={formData.name}
                  onChange={(e) => setFormData((d) => ({ ...d, name: e.target.value }))}
                  placeholder={t("skillVault.displayNamePlaceholder")}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="skill-desc">{t("common.description")}</Label>
                <Input
                  id="skill-desc"
                  value={formData.description}
                  onChange={(e) => setFormData((d) => ({ ...d, description: e.target.value }))}
                  placeholder={t("skillVault.descPlaceholder")}
                />
              </div>
            </div>
            <DialogFooter>
              <Button onClick={handleCreate} disabled={!formData.key || !formData.name || createSkill.isPending}>
                {createSkill.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                {t("common.create")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <Dialog open={!!publishSkillId} onOpenChange={(open) => !open && setPublishSkillId(null)}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t("skillVault.publishTitle")}</DialogTitle>
              <DialogDescription>{t("skillVault.publishDesc")}</DialogDescription>
            </DialogHeader>
            <div className="space-y-3">
              <div className="space-y-2">
                <Label htmlFor="version">Version (semver)</Label>
                <Input
                  id="version"
                  value={publishVersion_str}
                  onChange={(e) => setPublishVersionStr(e.target.value)}
                  placeholder={t("skillVault.versionPlaceholder")}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="artifact">{t("skillVault.artifactLabel")}</Label>
                <Input
                  id="artifact"
                  type="file"
                  onChange={(e) => setPublishFile(e.target.files?.[0] || null)}
                />
              </div>
            </div>
            <DialogFooter>
              <Button onClick={handlePublish} disabled={!publishVersion_str || !publishFile || publishVersion.isPending}>
                {publishVersion.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                {t("skillVault.publish")}
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
                    <TableCell><Skeleton className="h-4 w-40" /></TableCell>
                  </TableRow>
                ))
              ) : skills?.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="py-8 text-center text-muted-foreground">
                    <Archive className="mx-auto h-8 w-8 mb-2" />
                    {t("skillVault.noSkills")}
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
                      <div className="flex items-center justify-end gap-1">
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => {
                                setPublishSkillId(skill.id);
                                setPublishVersionStr("");
                                setPublishFile(null);
                              }}
                            >
                              <Upload className="mr-1 h-3 w-3" /> {t("skillVault.publish")}
                              <HelpCircle className="ml-1 h-3 w-3 text-muted-foreground" />
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent side="top" className="max-w-xs">
                            {t("skillVault.publishTip")}
                          </TooltipContent>
                        </Tooltip>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => handleInstall(skill.id)}
                              disabled={installSkill.isPending}
                            >
                              <Download className="mr-1 h-3 w-3" /> {t("skillVault.installFleet")}
                              <HelpCircle className="ml-1 h-3 w-3 text-muted-foreground" />
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent side="top" className="max-w-xs">
                            {t("skillVault.installFleetTip")}
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
