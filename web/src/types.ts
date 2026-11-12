export type Me = {
  user: { id: string; name: string; email: string; role: string };
  csrf: string;
  mfa_enabled: boolean;
  password_enabled: boolean;
  github_enabled: boolean;
  github_account: { id: number; login: string } | null;
  session: { id: string };
};
