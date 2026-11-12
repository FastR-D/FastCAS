export type Me = {
  user: { id: string; name: string; email: string; role: string };
  csrf: string;
  mfa_enabled: boolean;
  session: { id: string };
};
