export interface User {
  id: string;
  display_name: string;
  email: string;
  is_active: boolean;
  is_admin: boolean;
  created_at: string;
}

export interface TeamMembership {
  team_id: string;
  team_name: string;
  role: string;
}

export interface Team {
  id: string;
  name: string;
  description: string | null;
  created_at: string;
  role?: string;
}

export interface Session {
  id: string;
  created_at: string;
  expires_at: string;
  last_seen_at: string;
  user_agent: string | null;
  ip_address: string | null;
  is_current: boolean;
}

export interface ApiToken {
  id: string;
  name: string;
  created_at: string;
  last_used_at: string | null;
  expires_at: string | null;
}

export interface ApiTokenCreated extends ApiToken {
  token: string;
}
