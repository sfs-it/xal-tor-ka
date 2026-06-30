# Xal-Tor-Ka — तकनीकी टिप्पणियाँ (यह क्या करता है और कैसे)

*आधिकारिक अंग्रेज़ी दस्तावेज़ का अनुवाद; किसी भी अंतर की स्थिति में अंग्रेज़ी संस्करण मान्य होगा।*

यह कैसे काम करता है, इसकी एक तकनीकी पर पठनीय व्याख्या। **कठोर विनिर्देश** (JSON
स्कीमा, डेटा मॉडल, endpoint contract) के लिए देखें
[`BLUEPRINT.md`](../../BLUEPRINT.md); इसे इंस्टॉल करने के लिए देखें
[`REQUIREMENTS.md`](../../REQUIREMENTS.md) + [`INSTALL.md`](../../INSTALL.md)।

## एक वाक्य में

Xal-Tor-Ka एक **authentication gatekeeper + reverse-proxy manager** है: यह
**NGINX** को इंटरनेट पर उजागर एकमात्र सेवा के रूप में रखता है, और हर अनुरोध के लिए यह
एक **आंतरिक Go सेवा** (जो कभी उजागर नहीं होती) से पूछता है कि उसे पास करना है, लॉगिन
माँगना है, या अस्वीकार करना है।

```
Internet → NGINX (gatekeeper) ──auth_request (internal)──► Xal-Tor-Ka (Go)
                  │ 200 = pass / 401 = login / 403 = denied
                  └── proxy_pass (only if authorized) ──► internal backends
```

## यह कैसे तय करता है: `auth_request` प्रवाह

NGINX को **नियम पता नहीं होते**। हर आने वाले अनुरोध के लिए यह Go सेवा के `/validate`
endpoint पर एक आंतरिक *subrequest* करता है, और मूल host तथा path पास करता है। सेवा
एक HTTP स्थिति के साथ उत्तर देती है:

- **200** → NGINX असली backend की ओर `proxy_pass` के साथ आगे बढ़ता है;
- **401** → NGINX लॉगिन पेज पर रीडायरेक्ट करता है;
- **403** → एक्सेस अस्वीकृत।

सिद्धांत है **fail-closed**: मूल्यांकन के दौरान कोई भी त्रुटि, timeout या संदेह
401/403 देता है, **कभी** 200 नहीं। Go सेवा बाहर से पहुँच में नहीं है: केवल NGINX,
आंतरिक नेटवर्क पर, उससे क्वेरी कर सकता है।

## कोर: authorization matrix

प्रत्येक **host** और **path** के लिए, तीन नियमों में से एक लागू होता है:

| नियम | किसे प्रवेश मिलता है |
|------|-------------|
| `public` | कोई भी |
| `authenticated` | वैध session और पूर्ण **2FA** (TOTP) वाले उपयोगकर्ता |
| `whitelist` | केवल वे उपयोगकर्ता जो उस सेवा के लिए **स्पष्ट रूप से अधिकृत** हैं |

**Administrators** हर चीज़ तक पहुँच सकते हैं। ग्रैन्युलैरिटी प्रति subdomain और प्रति
path है (उदा. एक ही host पर `/` सार्वजनिक हो पर `/admin` whitelist हो)।

## प्रमाणीकरण (Authentication)

- **Local**: पासवर्ड **argon2id** से hash किए जाते हैं + **TOTP** के माध्यम से एक
  दूसरा कारक (RFC 6238, Google Authenticator/Authy के संगत)।
- **OIDC** (OpenID Connect): लॉगिन **Google**, **Microsoft/Entra** या सामान्य
  providers (**Keycloak, Authentik, Auth0, Okta, GitLab**) को सौंपा जाता है। identity
  token के signature की पुष्टि provider की public keys के विरुद्ध की जाती है।
  - **कोई auto-provisioning नहीं**: उपयोगकर्ता का पहले से मौजूद होना ज़रूरी है, उस
    provider के लिए घोषित — Google से साइन इन करना भीतर आने के लिए पर्याप्त नहीं है।
    देखें [`AUTH-PROVIDERS.md`](../../AUTH-PROVIDERS.md)।
- **Sessions**: `HttpOnly`/`SameSite=Lax` cookies (और HTTPS के पीछे `Secure`),
  फ़ाइल persistence के साथ RAM में रखे जाते हैं (वे restart के बाद भी बने रहते हैं)।

## कॉन्फ़िगरेशन: सब कुछ JSON में

कोई अनिवार्य डेटाबेस नहीं: कॉन्फ़िगरेशन कुछ typed JSON फ़ाइलों में रहता है, जिनकी
startup पर पुष्टि होती है (**Fail-Fast**: कोई अज्ञात field या रेंज से बाहर का मान
startup को एक स्पष्ट संदेश के साथ रोक देता है)।

| फ़ाइल | सामग्री |
|------|---------|
| `config.json` | इन्फ्रास्ट्रक्चर (env-templated): auth mode, TLS, sessions, admin IPs, providers |
| `secrets.json` | secrets (OIDC client secrets, tokens, SMTP) — कभी version नहीं किया जाता |
| `users.json` | उपयोगकर्ता, roles, 2FA, authorizations — कभी version नहीं किया जाता |
| `services.json` | runtime-प्रबंधित सेवाएँ (proxied backends + dashboard links) |

उपयोगकर्ताओं/सेवाओं में बदलाव **hot** (hot reload) लागू होते हैं, बिना restart के।

## घटक (Go सेवा)

Stdlib-first, static binary। मुख्य packages:

- `handlers/` — HTTP endpoints: `/validate`, login + TOTP, OIDC callback, setup,
  `/admin` पैनल, `/listing` डैशबोर्ड।
- `providers/` — प्रमाणीकरण: `local` और `oidc` (common interface)।
- `matrix/` — authorization नियमों का मूल्यांकन (प्रति host/path)।
- `proxy/` — NGINX backend vhosts उत्पन्न करता है और reload करता है।
- `health/` — आवधिक backend health checks (`/health` endpoint)।
- `config/` — load + validation + snapshots के साथ atomic save।
- `audit/` — विफल एक्सेस प्रयासों का लॉग (fail2ban के लिए)।
- `auth/` — hashing, TOTP, sessions, user directory।

## Reverse proxy: उत्पादन और reload

Manager NGINX backend कॉन्फ़िगरेशन उत्पन्न करता है (प्रति host एक `server{}`,
संरक्षित routes पर `auth_request` और upstream की ओर `proxy_pass` के साथ)। **reload**:

- **Docker**: NGINX container बदलाव का पता लगाता है और स्वयं को reload करता है
  (polling), क्योंकि Docker Desktop/WSL2 bind mounts पर `inotify` अविश्वसनीय है।
- **Host/LXD**: Go सेवा एक configurable reload command चलाती है
  (`nginx -s reload` / `systemctl reload nginx`)।

NGINX हमेशा नई कॉन्फ़िगरेशन की पुष्टि करता है और, यदि वह अमान्य है, तो चालू वाली को
बनाए रखता है: एक खराब regeneration proxy को बंद नहीं करता।

## प्रबंधन और परिचालन

- **`/admin` पैनल** (IP-प्रतिबंधित): सेवाओं, उपयोगकर्ताओं और अनुमतियों का प्रबंधन,
  स्थिति की निगरानी, अलग-अलग पेजों में।
- **`/listing` डैशबोर्ड**: प्रत्येक उपयोगकर्ता को केवल वे सेवाएँ दिखाता है जिन तक वह
  पहुँच सकता है।
- **Onboarding**: पहला run वेब के माध्यम से पहला administrator बनाने के लिए एक
  expiring token उत्पन्न करता है; फिर इंटरफ़ेस blindato (locked down) हो जाता है।
- **Backups**: हर save auto-trash के साथ एक snapshot बनाता है (अंतिम N रखता है),
  जिसमें CLI से भी restore शामिल है।
- **Brute-force बचाव**: विफल प्रयास असली client IP के साथ एक structured लॉग
  (`logs/auth.log`) में दर्ज होते हैं, जो **fail2ban** में प्लग करने योग्य है।

## एक नज़र में सुरक्षा

- उजागर एकमात्र सेवा **NGINX** है; Go सेवा आंतरिक है।
- पूरे authorization path में **Fail-closed**।
- Secret की तुलनाएँ **constant time** में; secrets **कभी** log नहीं होते।
- Admin क्षेत्र **IP** द्वारा प्रतिबंधित; असली client IP केवल विश्वसनीय proxies से
  `X-Forwarded-For` से लिया जाता है।
- setup token **single-use** और expiring है।

## परिनियोजन (Deployment)

- **Docker Compose** (डिफ़ॉल्ट): NGINX उजागर, Go सेवा आंतरिक, container discovery के
  लिए एक read-only sidecar, resource limits और log rotation।
- **Host / LXD / dedicated machine**: static binary + system NGINX, तीन variables
  (`DEPLOY_MODE`, `NGINX_RELOAD_CMD`, `UPSTREAM_LOCALHOST`) द्वारा नियंत्रित।
  देखें [`INSTALL.md`](../../INSTALL.md) §9।

## संस्करण (Version)

सत्य का एकमात्र स्रोत `version/version.go` में (pre-1.0 लाइन `beta0.N`), build समय पर
overridable, और `xaltorka version`, `/healthz`, startup लॉग तथा UI में दिखाया जाता
है।
