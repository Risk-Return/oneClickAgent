import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { useUIStore } from "@/store/uiStore";
import { TokenManager } from "@/auth/TokenManager";
import { apiClient } from "@/api/client";
import { useQuery } from "@tanstack/react-query";
import { Sun, Moon, Monitor } from "lucide-react";

export function SettingsPage() {
  const { t } = useTranslation();
  const { theme, setTheme } = useUIStore();
  const tokenManager = TokenManager.getInstance();
  const navigate = useNavigate();

  const { data: user, isLoading } = useQuery({
    queryKey: ["auth", "me"],
    queryFn: () => apiClient.get<{ email: string; username: string; role: string; tier: string }>("/auth/me"),
  });

  const handleLogout = async () => {
    await tokenManager.logout();
    navigate("/login");
  };

  return (
    <div className="space-y-6 p-6 max-w-2xl">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">{t("settings.title")}</h1>
        <p className="text-muted-foreground">{t("settings.desc")}</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{t("settings.profile")}</CardTitle>
          <CardDescription>{t("settings.profileDesc")}</CardDescription>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <p className="text-sm text-muted-foreground">Loading...</p>
          ) : (
            <div className="space-y-3">
              <div>
                <Label className="text-xs text-muted-foreground">{t("settings.email")}</Label>
                <p className="text-sm">{user?.email}</p>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">{t("settings.username")}</Label>
                <p className="text-sm">{user?.username}</p>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">{t("settings.role")}</Label>
                <p className="text-sm capitalize">{user?.role}</p>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">{t("settings.tier")}</Label>
                <p className="text-sm capitalize">{user?.tier}</p>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("settings.appearance")}</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex gap-2">
            <Button
              variant={theme === "light" ? "default" : "outline"}
              size="sm"
              onClick={() => setTheme("light")}
            >
              <Sun className="mr-2 h-4 w-4" /> {t("nav.theme.light")}
            </Button>
            <Button
              variant={theme === "dark" ? "default" : "outline"}
              size="sm"
              onClick={() => setTheme("dark")}
            >
              <Moon className="mr-2 h-4 w-4" /> {t("nav.theme.dark")}
            </Button>
            <Button
              variant={theme === "system" ? "default" : "outline"}
              size="sm"
              onClick={() => setTheme("system")}
            >
              <Monitor className="mr-2 h-4 w-4" /> {t("nav.theme.system")}
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-destructive">{t("settings.dangerZone")}</CardTitle>
        </CardHeader>
        <CardContent>
          <Button variant="destructive" onClick={handleLogout}>
            {t("settings.signOut")}
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
