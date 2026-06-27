import { Button } from "@pivox/primitives/button";
import { cn } from "@pivox/primitives/utils";
import {
  AppWindow,
  KeyRound,
  Link2,
  LogOut,
  MonitorSmartphone,
  User,
  UserRound,
  Users,
} from "lucide-react";

import { features } from "./env";
import { keycloak, logout } from "./keycloak";
import { AccountSecurity } from "./pages/account-security";
import { Applications } from "./pages/applications";
import { DeviceActivity } from "./pages/device-activity";
import { Groups } from "./pages/groups";
import { LinkedAccounts } from "./pages/linked-accounts";
import { PersonalInfo } from "./pages/personal-info";
import { navigate, useRoute, type RouteId } from "./router";

type NavItem = { id: RouteId; label: string; icon: typeof UserRound };

const NAV: NavItem[] = [
  { id: "personal-info", label: "Personal info", icon: UserRound },
  { id: "account-security", label: "Account security", icon: KeyRound },
  { id: "device-activity", label: "Device activity", icon: MonitorSmartphone },
  { id: "applications", label: "Applications", icon: AppWindow },
  ...(features.isLinkedAccountsEnabled
    ? [{ id: "linked-accounts" as const, label: "Linked accounts", icon: Link2 }]
    : []),
  ...(features.isViewGroupsEnabled
    ? [{ id: "groups" as const, label: "Groups", icon: Users }]
    : []),
];

const PAGES: Record<RouteId, () => React.JSX.Element> = {
  "personal-info": PersonalInfo,
  "account-security": AccountSecurity,
  "device-activity": DeviceActivity,
  applications: Applications,
  "linked-accounts": LinkedAccounts,
  groups: Groups,
};

export function App() {
  const route = useRoute();
  const claims = keycloak.tokenParsed as
    | { name?: string; preferred_username?: string }
    | undefined;
  const displayName = claims?.name ?? claims?.preferred_username ?? "";
  const Page = PAGES[route];

  return (
    <div className="flex min-h-screen flex-col">
      <header className="flex items-center justify-between border-b border-border px-6 py-3">
        <span className="text-base font-semibold">Pivox Account</span>
        <div className="flex items-center gap-3">
          {displayName && (
            <span className="flex items-center gap-2 text-sm text-muted-foreground">
              <User className="size-4" />
              {displayName}
            </span>
          )}
          <Button variant="outline" size="sm" onClick={logout}>
            <LogOut className="size-4" />
            Sign out
          </Button>
        </div>
      </header>

      <div className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-8 px-6 py-8 md:flex-row">
        <nav className="flex shrink-0 flex-row flex-wrap gap-1 md:w-52 md:flex-col md:flex-nowrap">
          {NAV.map((item) => {
            const Icon = item.icon;
            const active = route === item.id;
            return (
              <button
                key={item.id}
                type="button"
                onClick={() => navigate(item.id)}
                aria-current={active ? "page" : undefined}
                className={cn(
                  "flex items-center gap-2 rounded-lg px-3 py-2 text-left text-sm transition-colors",
                  active
                    ? "bg-muted font-medium text-foreground"
                    : "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
                )}
              >
                <Icon className="size-4" />
                {item.label}
              </button>
            );
          })}
        </nav>

        <main className="flex-1">
          <Page />
        </main>
      </div>
    </div>
  );
}
