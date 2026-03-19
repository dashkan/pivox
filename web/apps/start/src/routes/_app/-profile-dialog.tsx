import { UserProfileFeature } from '@pivox/features/user-profile'
import { useAppLayoutContext } from '@pivox/ui/app-layout'
import { UserProfileCard } from '@pivox/ui/user-profile-card'

export default function ProfileDialog() {
  const { state, actions } = useAppLayoutContext()

  return (
    <UserProfileFeature
      onClose={() => actions.setProfileOpen(false)}
      open={state.profileOpen}
    >
      <UserProfileCard.Root
        open={state.profileOpen}
        onOpenChange={actions.setProfileOpen}
      >
        <UserProfileCard.Sidebar />
        <UserProfileCard.AccountPage />
        <UserProfileCard.SecurityPage />
      </UserProfileCard.Root>
    </UserProfileFeature>
  )
}
