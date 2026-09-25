import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { AuthLayout, FormError, FormNote } from "../components/layout/AuthLayout";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";
import { errorMessage } from "../lib/errors";
import { useT } from "../lib/i18n";
import { useFragmentToken } from "../lib/useFragmentToken";

export function ConfirmEmailPage() {
  const t = useT();
  const token = useFragmentToken();
  const { user, setUser } = useAuth();
  const [email, setEmail] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(token ? null : t("auth.invalidLink"));
  const started = useRef(false);

  useEffect(() => {
    if (!token || started.current) return;
    started.current = true;
    api
      .confirmEmail(token)
      .then((u) => {
        setEmail(u.email);
        if (user && user.id === u.id) setUser(u);
      })
      .catch((err) => setError(errorMessage(err, t("auth.invalidLink"))));
  }, [token, user, setUser, t]);

  return (
    <AuthLayout title={t("auth.confirmEmailTitle")}>
      {email ? <FormNote>{t("auth.emailConfirmed", { email })}</FormNote> : error ? <FormError message={error} /> : <FormNote>{t("auth.confirming")}</FormNote>}
      <Link to={user ? "/account" : "/login"} className="text-center font-sans text-sm text-text-secondary hover:text-primary-text">
        {t("auth.continue")}
      </Link>
    </AuthLayout>
  );
}
