import { useTranslation } from "react-i18next";
import { useAdminAgents, useReleaseAgent, useDrainAgent } from "@/features/useAgents";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { AgentStatusBadge } from "@/components/AgentStatusBadge";
import { Bot, Unlink } from "lucide-react";

export function AgentPoolPage() {
  const { t } = useTranslation();
  const { data: agents, isLoading } = useAdminAgents();
  const releaseAgent = useReleaseAgent();
  const drainAgent = useDrainAgent();

  return (
    <div className="space-y-6 p-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t("agentPool.title")}</h1>
        <p className="text-muted-foreground">{t("agentPool.desc")}</p>
      </div>

      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Device</TableHead>
                <TableHead>Job</TableHead>
                <TableHead>Tags</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                Array.from({ length: 4 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                  </TableRow>
                ))
              ) : !agents || agents.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="py-8 text-center text-muted-foreground">
                    <Bot className="mx-auto h-8 w-8 mb-2" />
                    {t("agentPool.noAgents")}
                  </TableCell>
                </TableRow>
              ) : (
                agents.map((agent) => (
                  <TableRow key={agent.id}>
                    <TableCell className="font-medium">{agent.name}</TableCell>
                    <TableCell>
                      <AgentStatusBadge status={agent.status} />
                    </TableCell>
                    <TableCell className="text-muted-foreground font-mono text-xs">
                      {agent.device_id.slice(0, 8)}...
                    </TableCell>
                    <TableCell className="text-muted-foreground font-mono text-xs">
                      {agent.job_id ? `${agent.job_id.slice(0, 8)}...` : "—"}
                    </TableCell>
                    <TableCell>
                      {agent.tags?.map((tag) => (
                        <Badge key={tag} variant="secondary" className="mr-1 text-xs">{tag}</Badge>
                      ))}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-1">
                        {agent.status === "busy" && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => releaseAgent.mutate(agent.id)}
                            disabled={releaseAgent.isPending}
                          >
                            <Unlink className="mr-1 h-3 w-3" /> {t("agentPool.release")}
                          </Button>
                        )}
                        {(agent.status === "idle" || agent.status === "busy") && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => drainAgent.mutate(agent.id)}
                            disabled={drainAgent.isPending}
                          >
                            {t("agentPool.drain")}
                          </Button>
                        )}
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
