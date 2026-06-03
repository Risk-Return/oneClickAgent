import { useState } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useUIStore } from "@/store/uiStore";
import { TokenManager } from "@/auth/TokenManager";
import { apiClient } from "@/api/client";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Sun, Moon, Monitor, Loader2 } from "lucide-react";

export function SettingsPage() {
  const { theme, setTheme } = useUIStore();
  const tokenManager = TokenManager.getInstance();

  const { data: user, isLoading } = useQuery({
    queryKey: ["auth", "me"],
    queryFn: () => apiClient.get<{ email: string; username: string }>("/auth/me"),
  });

  const [passwordData, setPasswordData] = useState({
    current: "",
    new: "",
    confirm: "",
  });
  const [changingPassword, setChangingPassword] = useState(false);

  const handlePasswordChange = async () => {
    if (passwordData.new !== passwordData.confirm) {
      toast.error("Passwords do not match");
      return;
    }
    if (passwordData.new.length < 12) {
      toast.error("Password must be at least 12 characters");
      return;
    }
    setChangingPassword(true);
    try {
      toast.success("Password changed (API not yet implemented)");
      setPasswordData({ current: "", new: "", confirm: "" });
    } catch {
      toast.error("Failed to change password");
    } finally {
      setChangingPassword(false);
    }
  };

  const handleLogout = async () => {
    await tokenManager.logout();
    window.location.href = "/login";
  };

  return (
    <div className="space-y-6 p-6 max-w-2xl">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Settings</h1>
        <p className="text-muted-foreground">Manage your account.</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Profile</CardTitle>
          <CardDescription>Your account information.</CardDescription>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <p className="text-sm text-muted-foreground">Loading...</p>
          ) : (
            <div className="space-y-3">
              <div>
                <Label className="text-xs text-muted-foreground">Email</Label>
                <p className="text-sm">{user?.email}</p>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Username</Label>
                <p className="text-sm">{user?.username}</p>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Change Password</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="space-y-2">
            <Label htmlFor="current">Current Password</Label>
            <Input
              id="current"
              type="password"
              value={passwordData.current}
              onChange={(e) => setPasswordData((p) => ({ ...p, current: e.target.value }))}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="new">New Password</Label>
            <Input
              id="new"
              type="password"
              minLength={12}
              value={passwordData.new}
              onChange={(e) => setPasswordData((p) => ({ ...p, new: e.target.value }))}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="confirm">Confirm New Password</Label>
            <Input
              id="confirm"
              type="password"
              value={passwordData.confirm}
              onChange={(e) => setPasswordData((p) => ({ ...p, confirm: e.target.value }))}
            />
          </div>
          <Button onClick={handlePasswordChange} disabled={changingPassword || !passwordData.current || !passwordData.new}>
            {changingPassword ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            Change password
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Appearance</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex gap-2">
            <Button
              variant={theme === "light" ? "default" : "outline"}
              size="sm"
              onClick={() => setTheme("light")}
            >
              <Sun className="mr-2 h-4 w-4" /> Light
            </Button>
            <Button
              variant={theme === "dark" ? "default" : "outline"}
              size="sm"
              onClick={() => setTheme("dark")}
            >
              <Moon className="mr-2 h-4 w-4" /> Dark
            </Button>
            <Button
              variant={theme === "system" ? "default" : "outline"}
              size="sm"
              onClick={() => setTheme("system")}
            >
              <Monitor className="mr-2 h-4 w-4" /> System
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-destructive">Danger Zone</CardTitle>
        </CardHeader>
        <CardContent>
          <Button variant="destructive" onClick={handleLogout}>
            Sign out
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
