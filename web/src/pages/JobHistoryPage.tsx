import { useJobs } from "@/features/useJobs";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { AgentStatusBadge } from "@/components/AgentStatusBadge";
import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatDistanceToNow } from "date-fns";
import { Clock, ArrowUpRight } from "lucide-react";

export function JobHistoryPage() {
  const { data: jobs, isLoading } = useJobs();

  return (
    <div className="space-y-6 p-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Job History</h1>
        <p className="text-muted-foreground">Browse and review past jobs.</p>
      </div>

      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Command</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Submitted</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                Array.from({ length: 5 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-40" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-12" /></TableCell>
                  </TableRow>
                ))
              ) : jobs?.items?.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} className="text-center py-8 text-muted-foreground">
                    <Clock className="mx-auto h-8 w-8 mb-2" />
                    No jobs yet.
                  </TableCell>
                </TableRow>
              ) : (
                jobs?.items?.map((job) => (
                  <TableRow key={job.id}>
                    <TableCell className="max-w-[300px] truncate font-medium">
                      {job.command}
                    </TableCell>
                    <TableCell>
                      <AgentStatusBadge status={job.status} />
                    </TableCell>
                    <TableCell className="text-muted-foreground text-sm">
                      {job.submitted_at
                        ? formatDistanceToNow(new Date(job.submitted_at), { addSuffix: true })
                        : "-"}
                    </TableCell>
                    <TableCell className="text-right">
                      <Link to={`/jobs/${job.id}`}>
                        <Button variant="ghost" size="icon">
                          <ArrowUpRight className="h-4 w-4" />
                        </Button>
                      </Link>
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
