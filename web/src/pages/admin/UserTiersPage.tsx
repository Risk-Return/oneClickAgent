import { useState } from "react";
import { useTranslation } from "react-i18next";
import { apiClient } from "@/api/client";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { toast } from "sonner";
import { Users, Copy, Check } from "lucide-react";
import type { User } from "@/api/schemas";

function UUIDCell({ uuid }: { uuid: string }) {
  const [expanded, setExpanded] = useState(false);
  const [copied, setCopied] = useState(false);

  const copy = (e: React.MouseEvent) => {
    e.stopPropagation();
    navigator.clipboard.writeText(uuid);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <button
      type="button"
      className="font-mono text-xs text-left hover:text-foreground text-muted-foreground flex items-center gap-1 group"
      onClick={() => setExpanded(!expanded)}
      title={expanded ? "Click to collapse" : "Click to reveal full UUID"}
    >
      <span>{expanded ? uuid : uuid.slice(0, 8) + "..."}</span>
      <button
        type="button"
        onClick={copy}
        className="opacity-0 group-hover:opacity-100 transition-opacity"
        title="Copy UUID"
      >
        {copied ? <Check className="h-3 w-3 text-green-500" /> : <Copy className="h-3 w-3" />}
      </button>
    </button>
  );
}

export function UserTiersPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const { data: users, isLoading } = useQuery({
    queryKey: ["admin", "users"],
    queryFn: () => apiClient.getList<User>("/admin/users"),
  });

  const handleSetTier = async (userId: string, tier: string) => {
    try {
      await apiClient.patch(`/admin/users/${userId}/tier`, { tier });
      queryClient.invalidateQueries({ queryKey: ["admin", "users"] });
      toast.success(t("userTiers.tierSet", { tier }));
    } catch (err: unknown) {
      toast.error((err as { message?: string })?.message || t("userTiers.tierFailed"));
    }
  };

  const tierBadge = (tier: string) => {
    const variants: Record<string, "default" | "warning" | "success"> = {
      free: "default",
      pro: "warning",
      enterprise: "success",
    };
    return <Badge variant={variants[tier] || "default"}>{tier}</Badge>;
  };

  return (
    <div className="space-y-6 p-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t("userTiers.title")}</h1>
        <p className="text-muted-foreground">{t("userTiers.desc")}</p>
      </div>

      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>UUID</TableHead>
                <TableHead>Username</TableHead>
                <TableHead>Email</TableHead>
                <TableHead>Role</TableHead>
                <TableHead>Tier</TableHead>
                <TableHead>Change Tier</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                Array.from({ length: 3 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-40" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-12" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                  </TableRow>
                ))
              ) : !users || users.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="py-8 text-center text-muted-foreground">
                    <Users className="mx-auto h-8 w-8 mb-2" />
                    {t("userTiers.noUsers")}
                  </TableCell>
                </TableRow>
              ) : (
                users.map((user) => (
                  <TableRow key={user.id}>
                    <TableCell><UUIDCell uuid={user.id} /></TableCell>
                    <TableCell className="font-medium">{user.username}</TableCell>
                    <TableCell className="text-muted-foreground">{user.email}</TableCell>
                    <TableCell>
                      <Badge variant="outline">{user.role}</Badge>
                    </TableCell>
                    <TableCell>{tierBadge(user.tier)}</TableCell>
                    <TableCell>
                      <Select
                        value={user.tier}
                        onValueChange={(tier) => handleSetTier(user.id, tier)}
                      >
                        <SelectTrigger className="h-8 w-32">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="free">Free</SelectItem>
                          <SelectItem value="pro">Pro</SelectItem>
                          <SelectItem value="enterprise">Enterprise</SelectItem>
                        </SelectContent>
                      </Select>
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
