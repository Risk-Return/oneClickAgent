import { useVisibleSkills } from "@/features/useSkills";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Badge } from "@/components/ui/badge";
import { Puzzle } from "lucide-react";

export function SkillsPage() {
  const { data: skills, isLoading } = useVisibleSkills();

  return (
    <div className="space-y-6 p-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Skills</h1>
        <p className="text-muted-foreground">Skills available for your jobs. Select at most one per job. Skills must be installed on the pool's host devices to be usable.</p>
      </div>

      {isLoading ? (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Card key={i}>
              <CardHeader>
                <Skeleton className="h-5 w-32" />
              </CardHeader>
              <CardContent>
                <Skeleton className="h-12 w-full" />
              </CardContent>
            </Card>
          ))}
        </div>
      ) : skills && skills.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center gap-4 py-12">
            <Puzzle className="h-12 w-12 text-muted-foreground" />
            <p className="text-muted-foreground">No skills visible yet. An admin needs to grant you access.</p>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {skills?.map((skill) => (
            <Card key={skill.id}>
              <CardHeader>
                <div className="flex items-center justify-between">
                  <CardTitle className="text-base">{skill.name}</CardTitle>
                  <Badge variant={skill.visibility === "public" ? "success" : "secondary"}>
                    {skill.visibility}
                  </Badge>
                </div>
                <CardDescription className="text-xs font-mono">{skill.key}</CardDescription>
              </CardHeader>
              <CardContent>
                <p className="text-sm text-muted-foreground">{skill.description || "No description"}</p>
                {skill.latest_version && (
                  <p className="mt-2 text-xs text-muted-foreground">Version: {skill.latest_version}</p>
                )}
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
