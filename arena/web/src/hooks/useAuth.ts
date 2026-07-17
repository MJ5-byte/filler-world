import { useCallback, useEffect, useState } from 'react'
import { api, AuthUser } from '../api'

// Shared by App (gated shell) and Landing (needs to know if a visitor is
// already signed in, to skip the login prompt).
export function useAuth() {
  const [user, setUser] = useState<AuthUser | null>(null)
  const [authReady, setAuthReady] = useState(false)

  const refreshUser = useCallback(() => {
    api.me()
      .then(d => setUser(d.user))
      .catch(() => setUser(null))
      .finally(() => setAuthReady(true))
  }, [])

  useEffect(() => { refreshUser() }, [refreshUser])

  return { user, authReady, refreshUser, setUser }
}
