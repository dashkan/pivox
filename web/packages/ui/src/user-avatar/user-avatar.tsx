import { cn } from '@pivox/primitives/utils';
import { Avatar, AvatarFallback, AvatarImage } from '@pivox/primitives/avatar';

function getInitials(name: string | null | undefined): string {
  if (!name) return '?';
  const parts = name.trim().split(/\s+/).filter(Boolean);
  const first = parts[0]?.[0];
  const last = parts.length > 1 ? parts[parts.length - 1]?.[0] : undefined;
  if (!first) return '?';
  return last ? (first + last).toUpperCase() : first.toUpperCase();
}

export function UserAvatar({
  src,
  name,
  size,
  className,
}: {
  src?: string | null;
  name?: string | null;
  size?: 'sm' | 'default' | 'lg';
  className?: string;
}) {
  return (
    <Avatar size={size} className={cn(className)}>
      {src && <AvatarImage src={src} alt={name ?? 'User avatar'} />}
      <AvatarFallback>{getInitials(name)}</AvatarFallback>
    </Avatar>
  );
}
