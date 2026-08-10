import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@pivox/primitives/collapsible';
import {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
} from '@pivox/primitives/sidebar';
import { Link } from '@tanstack/react-router';
import { ChevronRightIcon } from 'lucide-react';

import { useAppShellContext } from './app-shell.context';

/**
 * Top-level nav item with optional subitems. `href` is an absolute
 * path; the feature hook is responsible for assembling correct route
 * strings (TanStack Router validates them at navigation time).
 */
export interface NavMainItem {
  title: string;
  href: string;
  icon?: React.ReactNode;
  /**
   * Whether the parent should render expanded by default. Wired to
   * the route-active state so the user's current section opens.
   */
  isActive?: boolean;
  items?: { title: string; href: string }[];
}

export function AppShellNavMain() {
  const { state } = useAppShellContext();

  return (
    <SidebarGroup>
      <SidebarGroupLabel>Platform</SidebarGroupLabel>
      <SidebarMenu>
        {state.navMain.map((item) => (
          <Collapsible
            render={
              <SidebarMenuItem>
                <CollapsibleTrigger
                  render={
                    <SidebarMenuButton tooltip={item.title}>
                      {item.icon}
                      <span>{item.title}</span>
                      <ChevronRightIcon className="ms-auto transition-transform duration-200 group-data-open/collapsible:rotate-90" />
                    </SidebarMenuButton>
                  }
                />
                <CollapsibleContent>
                  <SidebarMenuSub>
                    {item.items?.map((subItem) => (
                      <SidebarMenuSubItem key={subItem.title}>
                        <SidebarMenuSubButton
                          render={
                            <Link to={subItem.href}>
                              <span>{subItem.title}</span>
                            </Link>
                          }
                        />
                      </SidebarMenuSubItem>
                    ))}
                  </SidebarMenuSub>
                </CollapsibleContent>
              </SidebarMenuItem>
            }
            key={item.title}

            defaultOpen={item.isActive}
            className="group/collapsible"
          />
        ))}
      </SidebarMenu>
    </SidebarGroup>
  );
}
