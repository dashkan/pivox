"use client"

import { UserAvatar } from "@pivox/ui/user-avatar"
import { useAuth } from "@/auth/use-auth"

export function AuthAvatar({
  size,
  className,
}: {
  size?: "sm" | "default" | "lg"
  className?: string
}) {
  const { user, loading } = useAuth()

  if (loading) {
    return <UserAvatar name={null} size={size} className={className} />
  }

  return (
    <UserAvatar
      src={user?.photoURL}
      name={user?.displayName ?? user?.email}
      size={size}
      className={className}
    />
  )
}
