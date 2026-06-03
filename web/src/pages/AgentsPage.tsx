import { useAgents } from "@/features/useAgents";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { AgentStatusBadge } from "@/components/AgentStatusBadge";
import { ResourceBar } from "@/components/ResourceBar";
import { Badge } from "@/components/ui/badge";
import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Bot, ExternalLink } from "lucide-react";

export function AgentsPage() {
  const { data: agents, isLoading } = useAgents();

  return (
    <div className="space-y-6 p-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Agents</h1>
        <p className="text-muted-foreground">Agents currently allocated to your active jobs.</p>
      </div>

      {isLoading ? (
        <div className="grid gap-4 md:grid-cols-2">
          {Array.from({ length: 2 }).map((_, i) => (
            <Card key={i}>
              <CardHeader>
                <Skeleton className="h-5 w-32" />
              </CardHeader>
              <CardContent>
                <Skeleton className="h-20 w-full" />
              </CardContent>
            </Card>
          ))}
        </div>
      ) : agents && agents.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center gap-4 py-12">
            <Bot className="h-12 w-12 text-muted-foreground" />
            <p className="text-muted-foreground">No active agents. Submit a job to get one allocated.</p>
            <Link to="/jobs">
              <Button>Submit a job</Button>
            </Link>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {agents?.map((agent) => (
            <Card key={agent.id}>
              <CardHeader>
                <div className="flex items-center justify-between">
                  <CardTitle className="text-base">{agent.name}</CardTitle>
                  <AgentStatusBadge status={agent.status} />
                </div>
                <CardDescription>{agent.description || "No description"}</CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                <div className="flex flex-wrap gap-1">
                  {agent.tags?.map((tag) => (
                    <Badge key={tag} variant="secondary" className="text-xs">{tag}</Badge>
                  ))}
                </div>
                <ResourceBar label="CPU" used={0} total={agent.limits?.cpu || 2} unit="cores" />
                <ResourceBar label="Memory" used={0} total={agent.limits?.mem_mb || 4096} unit="MB" />
                {agent.job_id && (
                  <Link to={`/jobs/${agent.job_id}`}>
                    <Button variant="outline" size="sm">
                      <ExternalLink className="mr-2 h-3 w-3" /> View job
                    </Button>
                  </Link>
                )}
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
