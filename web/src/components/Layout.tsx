import { Outlet, NavLink, useNavigate } from "react-router-dom";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useUIStore } from "@/store/uiStore";
import { TokenManager } from "@/auth/TokenManager";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  LayoutDashboard,
  Terminal,
  Clock,
  Bot,
  Puzzle,
  Settings,
  Server,
  Archive,
  Rocket,
  Building2,
  Eye,
  Sun,
  Moon,
  Monitor,
  LogOut,
  Menu,
  ChevronLeft,
  User,
  Key,
} from "lucide-react";

interface NavItem {
  label: string;
  href: string;
  icon: React.ElementType;
  adminOnly?: boolean;
}

const customerNav: NavItem[] = [
  { label: "Dashboard", href: "/", icon: LayoutDashboard },
  { label: "Command", href: "/jobs", icon: Terminal },
  { label: "History", href: "/history", icon: Clock },
  { label: "Agents", href: "/agents", icon: Bot },
  { label: "Skills", href: "/skills", icon: Puzzle },
  { label: "Logins", href: "/logins", icon: Key },
];

const adminNav: NavItem[] = [
  { label: "Devices", href: "/admin/devices", icon: Server, adminOnly: true },
  { label: "Skill Vault", href: "/admin/skill-vault", icon: Archive, adminOnly: true },
  { label: "Fleet Rollout", href: "/admin/fleet-rollout", icon: Rocket, adminOnly: true },
  { label: "Organizations", href: "/admin/organizations", icon: Building2, adminOnly: true },
  { label: "Visibility", href: "/admin/visibility", icon: Eye, adminOnly: true },
];

export function Layout() {
  const navigate = useNavigate();
  const { theme, setTheme, sidebarOpen, toggleSidebar } = useUIStore();
  const tokenManager = TokenManager.getInstance();
  const isAdmin = tokenManager.getUserRole() === "admin";

  const handleLogout = async () => {
    await tokenManager.logout();
    navigate("/login");
  };

  const navItems = [...customerNav, ...(isAdmin ? adminNav : [])];

  return (
    <div className="flex h-screen overflow-hidden">
      <aside
        className={cn(
          "flex flex-col border-r bg-card transition-all duration-300",
          sidebarOpen ? "w-60" : "w-16"
        )}
      >
        <div className={cn("flex h-14 items-center border-b px-4", sidebarOpen ? "justify-between" : "justify-center")}>
          {sidebarOpen && <span className="text-lg font-bold tracking-tight">IAgent</span>}
          <Button variant="ghost" size="icon" onClick={toggleSidebar}>
            {sidebarOpen ? <ChevronLeft className="h-4 w-4" /> : <Menu className="h-4 w-4" />}
          </Button>
        </div>

        <ScrollArea className="flex-1 py-2">
          <nav className="flex flex-col gap-1 px-2">
            {navItems.map((item) =>
              item.adminOnly && !isAdmin ? null : (
                <NavLink
                  key={item.href}
                  to={item.href}
                  end={item.href === "/"}
                  className={({ isActive }) =>
                    cn(
                      "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors",
                      isActive
                        ? "bg-primary/10 text-primary"
                        : "text-muted-foreground hover:bg-accent hover:text-foreground",
                      !sidebarOpen && "justify-center px-2"
                    )
                  }
                >
                  <item.icon className="h-4 w-4 shrink-0" />
                  {sidebarOpen && <span>{item.label}</span>}
                </NavLink>
              )
            )}
          </nav>
        </ScrollArea>

        <Separator />
        <div className="p-2">
          <NavLink
            to="/settings"
            className={({ isActive }) =>
              cn(
                "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors",
                isActive
                  ? "bg-primary/10 text-primary"
                  : "text-muted-foreground hover:bg-accent hover:text-foreground",
                !sidebarOpen && "justify-center px-2"
              )
            }
          >
            <Settings className="h-4 w-4 shrink-0" />
            {sidebarOpen && <span>Settings</span>}
          </NavLink>
        </div>
      </aside>

      <div className="flex flex-1 flex-col overflow-hidden">
        <header className="flex h-14 items-center justify-between border-b px-6">
          <div className="flex-1" />
          <div className="flex items-center gap-2">
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon">
                  {theme === "dark" ? (
                    <Moon className="h-4 w-4" />
                  ) : theme === "light" ? (
                    <Sun className="h-4 w-4" />
                  ) : (
                    <Monitor className="h-4 w-4" />
                  )}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={() => setTheme("light")}>
                  <Sun className="mr-2 h-4 w-4" /> Light
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => setTheme("dark")}>
                  <Moon className="mr-2 h-4 w-4" /> Dark
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => setTheme("system")}>
                  <Monitor className="mr-2 h-4 w-4" /> System
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>

            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon">
                  <User className="h-4 w-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-48">
                <DropdownMenuLabel>Account</DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={() => navigate("/settings")}>
                  <Settings className="mr-2 h-4 w-4" /> Settings
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={handleLogout}>
                  <LogOut className="mr-2 h-4 w-4" /> Logout
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </header>

        <main className="flex-1 overflow-auto">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
