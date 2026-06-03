import { useAdminSkills, useDisableSkillFleet, useEnableSkillFleet, useDeleteSkillFleet } from "@/features/useSkills";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Rocket, Pause, Play, Trash2 } from "lucide-react";

export function FleetRolloutPage() {
  const { data: skills, isLoading } = useAdminSkills();
  const disableSkill = useDisableSkillFleet();
  const enableSkill = useEnableSkillFleet();
  const deleteSkill = useDeleteSkillFleet();

  return (
    <div className="space-y-6 p-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Fleet Rollout</h1>
        <p className="text-muted-foreground">Manage skills across all devices and agents in the fleet.</p>
      </div>

      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Skill</TableHead>
                <TableHead>Key</TableHead>
                <TableHead>Version</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                Array.from({ length: 3 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-12" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                  </TableRow>
                ))
              ) : skills?.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} className="py-8 text-center text-muted-foreground">
                    <Rocket className="mx-auto h-8 w-8 mb-2" />
                    No skills to manage.
                  </TableCell>
                </TableRow>
              ) : (
                skills?.map((skill) => (
                  <TableRow key={skill.id}>
                    <TableCell className="font-medium">{skill.name}</TableCell>
                    <TableCell className="font-mono text-xs text-muted-foreground">{skill.key}</TableCell>
                    <TableCell className="text-muted-foreground">{skill.latest_version || "-"}</TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => disableSkill.mutate(skill.id)}
                          disabled={disableSkill.isPending}
                        >
                          <Pause className="mr-1 h-3 w-3" /> Disable
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => enableSkill.mutate(skill.id)}
                          disabled={enableSkill.isPending}
                        >
                          <Play className="mr-1 h-3 w-3" /> Enable
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => deleteSkill.mutate(skill.id)}
                          disabled={deleteSkill.isPending}
                        >
                          <Trash2 className="mr-1 h-3 w-3 text-destructive" /> Delete
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
