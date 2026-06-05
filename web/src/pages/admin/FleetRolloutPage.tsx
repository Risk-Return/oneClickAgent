import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useAdminSkills, useSkillRollout, useRetrySkillFleet, useInstallSkillFleet } from "@/features/useSkills";
import { useDisableSkillFleet, useEnableSkillFleet, useDeleteSkillFleet } from "@/features/useSkills";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { AgentStatusBadge } from "@/components/AgentStatusBadge";
import { Rocket, Pause, Play, Trash2, ChevronDown, ChevronRight, RefreshCw } from "lucide-react";
import type { SkillRolloutEntry } from "@/api/schemas";

function RolloutDetail({ skillId }: { skillId: string }) {
  const { t } = useTranslation();
  const { data: entries, isLoading } = useSkillRollout(skillId);
  const retrySkill = useRetrySkillFleet();
  const [expandedDevices, setExpandedDevices] = useState<Set<string>>(new Set());

  function toggleDevice(deviceId: string) {
    setExpandedDevices((prev) => {
      const next = new Set(prev);
      if (next.has(deviceId)) {
        next.delete(deviceId);
      } else {
        next.add(deviceId);
      }
      return next;
    });
  }

  if (isLoading) {
    return <Skeleton className="h-20 w-full" />;
  }

  if (!entries || entries.length === 0) {
    return (
      <div className="py-4 text-center text-sm text-muted-foreground border-t">
        {t("fleetRollout.noDevices")}
      </div>
    );
  }

  return (
    <div className="border-t">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-8" />
            <TableHead>{t("fleetRollout.device")}</TableHead>
            <TableHead>{t("fleetRollout.version")}</TableHead>
            <TableHead>{t("fleetRollout.status")}</TableHead>
            <TableHead className="text-right">{t("fleetRollout.actions")}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {entries.map((entry) => (
            <DeviceRow
              key={entry.device_id}
              entry={entry}
              expanded={expandedDevices.has(entry.device_id)}
              onToggle={() => toggleDevice(entry.device_id)}
              onRetry={(agentId) =>
                retrySkill.mutate({
                  skillId,
                  deviceId: entry.device_id,
                  agentIds: agentId ? [agentId] : undefined,
                })
              }
              retrying={retrySkill.isPending}
            />
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

function DeviceRow({
  entry,
  expanded,
  onToggle,
  onRetry,
  retrying,
}: {
  entry: SkillRolloutEntry;
  expanded: boolean;
  onToggle: () => void;
  onRetry: (agentId?: string) => void;
  retrying: boolean;
}) {
  const { t } = useTranslation();
  const hasAgents = entry.agents && entry.agents.length > 0;
  const failedAgents = entry.agents?.filter((a) => a.status === "disabled" || a.status === "error") ?? [];
  const hasFailed = failedAgents.length > 0 || entry.status === "error";

  return (
    <>
      <TableRow className="hover:bg-muted/50">
        <TableCell>
          {hasAgents && (
            <Button variant="ghost" size="sm" className="h-6 w-6 p-0" onClick={onToggle}>
              {expanded ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
            </Button>
          )}
        </TableCell>
        <TableCell className="font-medium">{entry.device_name}</TableCell>
        <TableCell className="font-mono text-xs text-muted-foreground">{entry.version || "-"}</TableCell>
        <TableCell>
          <AgentStatusBadge status={entry.status} />
          {entry.error && (
            <span className="ml-2 text-xs text-destructive">{entry.error}</span>
          )}
        </TableCell>
        <TableCell className="text-right">
          {hasFailed && (
            <Button variant="ghost" size="sm" onClick={() => onRetry()} disabled={retrying}>
              <RefreshCw className="mr-1 h-3 w-3" />
              {t("fleetRollout.retry")}
            </Button>
          )}
        </TableCell>
      </TableRow>
      {expanded && hasAgents && (
        <TableRow>
          <TableCell colSpan={5} className="p-0">
            <div className="bg-muted/30 pl-10 pr-4 py-2">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="text-xs">{t("fleetRollout.agent")}</TableHead>
                    <TableHead className="text-xs">{t("fleetRollout.agentStatus")}</TableHead>
                    <TableHead className="text-xs text-right">{t("fleetRollout.agentActions")}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {(entry.agents ?? []).map((agent) => (
                    <TableRow key={agent.agent_id}>
                      <TableCell className="font-mono text-xs">{agent.agent_name}</TableCell>
                      <TableCell>
                        <AgentStatusBadge status={agent.status} />
                        {agent.error && (
                          <span className="ml-2 text-xs text-destructive">{agent.error}</span>
                        )}
                      </TableCell>
                      <TableCell className="text-right">
                        {(agent.status === "disabled" || agent.status === "error") && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => onRetry(agent.agent_id)}
                            disabled={retrying}
                          >
                            <RefreshCw className="mr-1 h-3 w-3" />
                            {t("fleetRollout.retry")}
                          </Button>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </TableCell>
        </TableRow>
      )}
    </>
  );
}

export function FleetRolloutPage() {
  const { t } = useTranslation();
  const { data: skills, isLoading } = useAdminSkills();
  const disableSkill = useDisableSkillFleet();
  const enableSkill = useEnableSkillFleet();
  const deleteSkill = useDeleteSkillFleet();
  const installSkill = useInstallSkillFleet();
  const [expandedSkill, setExpandedSkill] = useState<string | null>(null);

  return (
    <div className="space-y-6 p-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t("fleetRollout.title")}</h1>
        <p className="text-muted-foreground">{t("fleetRollout.desc")}</p>
      </div>

      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-8" />
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
                    <TableCell><Skeleton className="h-4 w-4" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-12" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                  </TableRow>
                ))
              ) : skills?.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={5} className="py-8 text-center text-muted-foreground">
                    <Rocket className="mx-auto h-8 w-8 mb-2" />
                    {t("fleetRollout.noSkills")}
                  </TableCell>
                </TableRow>
              ) : (
                skills?.map((skill) => {
                  const isExpanded = expandedSkill === skill.id;
                  return (
                    <>
                      <TableRow
                        key={skill.id}
                        className="cursor-pointer hover:bg-muted/50"
                        onClick={() => setExpandedSkill(isExpanded ? null : skill.id)}
                      >
                        <TableCell>
                          {isExpanded ? (
                            <ChevronDown className="h-4 w-4" />
                          ) : (
                            <ChevronRight className="h-4 w-4" />
                          )}
                        </TableCell>
                        <TableCell className="font-medium">{skill.name}</TableCell>
                        <TableCell className="font-mono text-xs text-muted-foreground">{skill.key}</TableCell>
                        <TableCell className="text-muted-foreground">{skill.latest_version || "-"}</TableCell>
                        <TableCell className="text-right">
                          <div className="flex items-center justify-end gap-1">
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={(e) => {
                                e.stopPropagation();
                                installSkill.mutate(skill.id);
                              }}
                              disabled={installSkill.isPending}
                            >
                              <Rocket className="mr-1 h-3 w-3" /> {t("fleetRollout.install")}
                            </Button>
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={(e) => {
                                e.stopPropagation();
                                disableSkill.mutate(skill.id);
                              }}
                              disabled={disableSkill.isPending}
                            >
                              <Pause className="mr-1 h-3 w-3" /> {t("fleetRollout.disable")}
                            </Button>
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={(e) => {
                                e.stopPropagation();
                                enableSkill.mutate(skill.id);
                              }}
                              disabled={enableSkill.isPending}
                            >
                              <Play className="mr-1 h-3 w-3" /> {t("fleetRollout.enable")}
                            </Button>
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={(e) => {
                                e.stopPropagation();
                                deleteSkill.mutate(skill.id);
                              }}
                              disabled={deleteSkill.isPending}
                            >
                              <Trash2 className="mr-1 h-3 w-3 text-destructive" /> {t("fleetRollout.delete")}
                            </Button>
                          </div>
                        </TableCell>
                      </TableRow>
                      {isExpanded && (
                        <TableRow>
                          <TableCell colSpan={5} className="p-0">
                            <RolloutDetail skillId={skill.id} />
                          </TableCell>
                        </TableRow>
                      )}
                    </>
                  );
                })
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}
