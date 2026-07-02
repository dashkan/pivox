import { Button } from "@pivox/primitives/button";
import { cn } from "@pivox/primitives/utils";
import {
  AppWindow,
  KeyRound,
  LogOut,
  User,
  UserRound,
  Users,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { NavLink, Outlet } from "react-router-dom";

import { features } from "./env";
import { keycloak, logout } from "./keycloak";

import type { ComponentType } from "react";

function NavItem({
  to,
  end,
  icon: Icon,
  label,
}: {
  to: string;
  end?: boolean;
  icon?: ComponentType<{ className?: string }>;
  label: string;
}) {
  return (
    <NavLink
      to={to}
      end={end}
      className={({ isActive }) =>
        cn(
          "flex items-center gap-2 rounded-lg px-3 py-2 text-left text-sm transition-colors",
          isActive
            ? "bg-muted font-medium text-foreground"
            : "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
        )
      }
    >
      {Icon && <Icon className="size-4" />}
      {label}
    </NavLink>
  );
}

export function App() {
  const { t } = useTranslation();
  const claims = keycloak.tokenParsed as
    | { name?: string; preferred_username?: string }
    | undefined;
  const displayName = claims?.name ?? claims?.preferred_username ?? "";

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
            {t("doSignOut")}
          </Button>
        </div>
      </header>

      <div className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-8 px-6 py-8 md:flex-row">
        <nav className="flex shrink-0 flex-col gap-1 md:w-52">
          <NavItem to="/" end icon={UserRound} label={t("personalInfoSidebarTitle")} />

          <div className="mt-2 flex flex-col gap-1">
            <span className="flex items-center gap-2 px-3 py-2 text-sm font-medium text-foreground">
              <KeyRound className="size-4" />
              {t("accountSecuritySidebarTitle")}
            </span>
            <div className="ml-4 flex flex-col gap-1">
              <NavItem
                to="/account-security/signing-in"
                label={t("signingInSidebarTitle")}
              />
              <NavItem
                to="/account-security/device-activity"
                label={t("deviceActivitySidebarTitle")}
              />
              {features.isLinkedAccountsEnabled && (
                <NavItem
                  to="/account-security/linked-accounts"
                  label={t("linkedAccountsSidebarTitle")}
                />
              )}
            </div>
          </div>

          <NavItem to="/applications" icon={AppWindow} label={t("applications")} />
          {features.isViewGroupsEnabled && (
            <NavItem to="/groups" icon={Users} label={t("groups")} />
          )}
        </nav>

        <main className="flex-1">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
