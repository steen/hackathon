import type * as React from "react";
import { useEffect, useState } from "react";
import { useAuth } from "./auth/AuthContext.js";
import { ErrorBanner } from "./components/ErrorBanner.js";
import { Login } from "./routes/Login.js";
import { Register } from "./routes/Register.js";
import { Chat } from "./routes/Chat.js";

type Route = "login" | "register" | "chat";

function readRoute(): Route {
  if (typeof window === "undefined") return "login";
  const h = window.location.hash.replace(/^#/, "");
  if (h === "/register") return "register";
  if (h === "/login") return "login";
  return "chat";
}

function setHash(path: string): void {
  if (typeof window === "undefined") return;
  window.location.hash = path;
}

export function App(): React.JSX.Element {
  const { token, user, loading } = useAuth();
  const [route, setRoute] = useState<Route>(() => readRoute());

  useEffect(() => {
    function onChange(): void {
      setRoute(readRoute());
    }
    window.addEventListener("hashchange", onChange);
    return () => {
      window.removeEventListener("hashchange", onChange);
    };
  }, []);

  if (loading) {
    return <div className="loading">Loading...</div>;
  }

  let body: React.JSX.Element;
  if (token === null || user === null) {
    if (route === "register") {
      body = (
        <Register
          onSwitchToLogin={() => {
            setHash("/login");
          }}
        />
      );
    } else {
      body = (
        <Login
          onSwitchToRegister={() => {
            setHash("/register");
          }}
        />
      );
    }
  } else {
    body = <Chat />;
  }

  return (
    <>
      <ErrorBanner />
      {body}
    </>
  );
}
