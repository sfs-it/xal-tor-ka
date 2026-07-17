# Paper next-gen — Fonti di autenticazione pluggable (Local · OIDC · LDAP/AD · PAM)

> **Stato:** vivo. **Autore:** custode (Xal-Tor-Ka). **Contesto:** beta0.6.
> **In una riga:** un solo gate, molte fonti d'identità dietro un'unica interfaccia `Provider`.

---

## 1. La visione

Xal-Tor-Ka autentica al bordo e proxa verso i servizi interni. Il valore cresce con il numero di
**fonti d'identità** che sa consultare senza cambiare il resto. Il disegno tiene le fonti dietro
**un'unica astrazione** (`providers.Provider`), così aggiungerne una è *additivo*: nuovo file,
zero refactor.

Due famiglie di fonti:

- **Credential-based** (l'utente inserisce **utente + password** nel form del gate, il gate
  valida): `local`, **`ldap`**, **`pam`**. Entrano tutte nello **stesso punto** del login flow.
- **Redirect-based** (il gate rimbalza a un IdP che restituisce un token): `oidc`.

## 2. Il modello `Provider`

```
type Provider interface { ID() string; Type() string }
// le credential-based aggiungono: Authenticate(user, password) (Identity, error)
```

Il login (`handlers/login.go`) prova le fonti credential-based in ordine: **Local**, poi le
altre abilitate. Prima risposta valida → sessione. Errore sempre **generico**
(`ErrInvalidCredentials`) per non fare user-enumeration. OIDC resta sul suo flusso redirect.

## 3. Le fonti

| Fonte | Stato | Come | Copre |
|---|---|---|---|
| **Local** | ✅ | argon2id su `users.json` | utenti interni |
| **OIDC** | ✅ | Authorization Code + discovery | Google/Microsoft/generico, Entra ID, ADFS |
| **LDAP** | ✅ **beta0.6** | bind LDAPS/StartTLS | **Active Directory (dominio)** + qualsiasi directory LDAP |
| **PAM** | 🟡 roadmap | delega allo stack PAM del container | Linux locale + (via SSSD/pam_ldap/Kerberos) LDAP/AD |

### 3.1 LDAP (implementato)

**Autenticazione = bind.** Il gate si connette al server LDAP (LDAPS `:636` o LDAP+StartTLS
`:389`) e prova un **bind** con le credenziali dell'utente. Bind riuscito = credenziali valide.

Due modalità:
- **Direct bind** (semplice, consigliata per AD): un **template di DN** con `%s` per lo username.
  - AD via UPN: `bind_dn_template = "%s@corp.example.com"` (l'utente digita `mario.rossi`).
  - LDAP classico: `bind_dn_template = "uid=%s,ou=people,dc=example,dc=com"`.
- **Search-then-bind** (per directory dove il DN non è derivabile): un service-account fa la
  ricerca dell'utente (`user_filter`, es. `(sAMAccountName=%s)`) sotto `base_dn`, poi bind col DN
  trovato. *(Estensione naturale; la beta0.6 spedisce il direct-bind, lo scheletro c'è.)*

**Sicurezza:** **sempre TLS** (LDAPS o StartTLS); `insecure_skip_verify` solo per lab. Nessuna
password in chiaro sulla rete, nessuna memorizzata dal gate.

**Autorizzazione (authz) via gruppi:** dopo l'auth, si può leggere l'appartenenza ai gruppi
(`memberOf` in AD) per decidere **admin** e **accesso ai backend** (es. gruppo `xtk-admins` →
admin; `xtk-users` → regola `authenticated`). *(La beta0.6 autentica; il mapping-gruppi fine è
il prossimo incremento — di default gli utenti LDAP sono non-admin e seguono le regole di
accesso dei backend.)*

**Config (`ProviderCfg`, type `ldap`):**
```
{ "id":"corp", "type":"ldap", "enabled":true,
  "ldap_url":"ldaps://dc1.corp.example.com:636",
  "ldap_bind_dn_template":"%s@corp.example.com",
  "ldap_base_dn":"dc=corp,dc=example,dc=com",   // opzionale, per search/gruppi
  "ldap_start_tls":false, "ldap_insecure_skip_verify":false }
```

### 3.2 PAM (roadmap)

**Delega allo stack PAM** del container: `pam_unix` (utenti Linux locali), oppure — via
`pam_sss`/`pam_ldap`/`pam_krb5` — **SSSD/LDAP/Kerberos**, quindi **anche AD**. Valore: riusare lo
stack di auth già configurato su un host.

**Perché è in roadmap e non nella beta0.6:**
- richiede **cgo + libpam** e la configurazione PAM **dentro il container** (o mount dell'host);
- per AD **finisce comunque su SSSD/LDAP** → si sovrappone a LDAP, che è più pulito (Go puro);
- ergo: **LDAP prima** (il grosso del valore enterprise senza cgo), **PAM poi** solo se serve il
  riuso dello stack PAM dell'host.

## 4. Active Directory e Windows — cosa si può e cosa no

- **Account di DOMINIO (Active Directory): SÌ.** AD *è* un server LDAP (+ Kerberos). Bind LDAPS al
  domain controller = standard enterprise. Coperti da **LDAP** (o PAM+SSSD/Kerberos). Con i
  gruppi AD (`memberOf`) si guida anche l'authorization.
- **Account LOCALI di Windows: NO** (da un gate Linux/docker). Il **SAM** di Windows **non è
  esposto** via LDAP/PAM: gli account locali Windows non sono raggiungibili da Linux senza un
  **broker lato-Windows** (SMB/NTLM o un agent nativo) — fuori scope. Gli account di **dominio**
  sì (sono AD), i **locali** no.

## 5. Roadmap

1. **beta0.6 — LDAP** (direct-bind, LDAPS/StartTLS). ✅
2. **LDAP search-then-bind** + **mapping gruppi → admin/accesso**.
3. **PAM** (cgo/libpam) — per il riuso dello stack host (Linux locale + SSSD/AD).
4. **Token/API-key** per client automatici (es. un client API davanti a un LLM che non fa il
   form di login — cfr. il caso *ollama-gatekeeper*).

## 6. Perché conta

Ogni fonte in più = un cancello che copre un pezzo d'infrastruttura in più **senza cambiare il
resto**. Con LDAP/AD, Xal-Tor-Ka smette di essere «un gate con utenti suoi» e diventa **il
front-door aziendale**: gli utenti di dominio loggano una volta e raggiungono i servizi interni —
API, dashboard, LLM — con l'accesso deciso dai gruppi. È il salto da homelab a enterprise.

---

*SFS.it · Apache-2.0. Companion: `docs/why-xaltorka.md`, `AUTH-PROVIDERS.md`.*
