import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useAgents } from "@/features/useAgents";
import { useJobs } from "@/features/useJobs";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { AgentStatusBadge } from "@/components/AgentStatusBadge";
import { Bot, Terminal, Clock, CheckCircle2, XCircle } from "lucide-react";

export function DashboardPage() {
  const { t } = useTranslation();
  const { data: agents, isLoading: agentsLoading } = useAgents();
  const { data: jobs, isLoading: jobsLoading } = useJobs();

  const activeAgents = agents?.filter((a) => a.status === "busy").length || 0;
  const recentJobs = jobs?.items?.slice(0, 5) || [];
  const succeededCount = recentJobs.filter((j) => j.status === "succeeded").length;
  const failedCount = recentJobs.filter((j) => j.status === "failed").length;

  return (
    <div className="space-y-6 p-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t("dashboard.title")}</h1>
        <p className="text-muted-foreground">{t("dashboard.welcome")}</p>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">{t("dashboard.activeAgents")}</CardTitle>
            <Bot className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {agentsLoading ? (
              <Skeleton className="h-8 w-12" />
            ) : (
              <p className="text-2xl font-bold">{activeAgents}</p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">{t("dashboard.recentJobs")}</CardTitle>
            <Terminal className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            {jobsLoading ? (
              <Skeleton className="h-8 w-12" />
            ) : (
              <p className="text-2xl font-bold">{jobs?.items?.length || 0}</p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">{t("dashboard.succeeded")}</CardTitle>
            <CheckCircle2 className="h-4 w-4 text-emerald-500" />
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold">{succeededCount}</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">{t("dashboard.failed")}</CardTitle>
            <XCircle className="h-4 w-4 text-red-500" />
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-bold">{failedCount}</p>
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-6 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>{t("dashboard.recentJobs")}</CardTitle>
            <CardDescription>{t("dashboard.recentJobsDesc")}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {jobsLoading ? (
              Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-10 w-full" />)
            ) : recentJobs.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t("dashboard.noJobsYet")}</p>
            ) : (
              recentJobs.map((job) => (
                <div key={job.id} className="flex items-center justify-between rounded-md border p-3">
                  <div className="flex items-center gap-3">
                    <AgentStatusBadge status={job.status} />
                    <span className="truncate text-sm max-w-[200px]">{job.command}</span>
                  </div>
                  <Link to={`/jobs/${job.id}`}>
                    <Button variant="ghost" size="sm">{t("dashboard.view")}</Button>
                  </Link>
                </div>
              ))
            )}
            <Link to="/history">
              <Button variant="outline" size="sm" className="w-full">
                <Clock className="mr-2 h-4 w-4" /> {t("dashboard.viewAllJobs")}
              </Button>
            </Link>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{t("dashboard.quickActions")}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <Link to="/jobs">
              <Button className="w-full">
                <Terminal className="mr-2 h-4 w-4" /> {t("dashboard.newCommand")}
              </Button>
            </Link>
            <Link to="/agents">
              <Button variant="outline" className="w-full">
                <Bot className="mr-2 h-4 w-4" /> {t("dashboard.viewAgents")}
              </Button>
            </Link>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
