import ProtectAppShell from '@/components/ProtectAppShell/ProtectAppShell';

export default function ProtectedLayout({ children }: LayoutProps<'/protected'>) {
  return <ProtectAppShell>{children}</ProtectAppShell>;
}
