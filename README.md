<div align="center">

```
    ██╗  ██╗███████╗██████╗ ███╗   ███╗███████╗███╗   ██╗███████╗██╗   ██╗███████╗
    ██║  ██║██╔════╝██╔══██╗████╗ ████║██╔════╝████╗  ██║██╔════╝██║   ██║██╔════╝
    ███████║█████╗  ██████╔╝██╔████╔██║█████╗  ██╔██╗ ██║█████╗  ██║   ██║███████╗
    ██╔══██║██╔══╝  ██╔══██╗██║╚██╔╝██║██╔══╝  ██║╚██╗██║██╔══╝  ██║   ██║╚════██║
    ██║  ██║███████╗██║  ██║██║ ╚═╝ ██║███████╗██║ ╚████║███████╗╚██████╔╝███████║
    ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚═╝     ╚═╝╚══════╝╚═╝  ╚═══╝╚══════╝ ╚═════╝ ╚══════╝
```

### *The Interpreter Between Two Tongues*

---

**A ClickHouse-Native Wire Server That Speaks StarRocks**

[![Author](https://img.shields.io/badge/Author-Yumiko%20Sturluson-ff69b4?style=for-the-badge)](https://github.com/yumikokawaii)
[![License](https://img.shields.io/badge/License-Private-9370DB?style=for-the-badge)]()
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=for-the-badge&logo=go&logoColor=white)]()
[![Protocol](https://img.shields.io/badge/ClickHouse%20Native-FFCC01?style=for-the-badge&logo=clickhouse&logoColor=black)]()
[![Backend](https://img.shields.io/badge/StarRocks-1E88E5?style=for-the-badge)]()

</div>

---

## About

> *"Hermes carried the words of the gods to mortals, and neither side ever knew he had changed the language."*

Welcome to **hermeneus**!

[Coroot](https://github.com/coroot/coroot) stores its logs, traces, and profiles in ClickHouse and speaks nothing
else. `hermeneus` sits where ClickHouse would be, accepts the native TCP protocol, and quietly carries every query to
**StarRocks** instead. Coroot runs **upstream, unmodified, forever** — no fork, no patch to re-apply on each release.

```
  coroot ──ClickHouse native TCP──▶ hermeneus ──MySQL wire / Stream Load / Kafka──▶ StarRocks
```

Translation is deliberate, not clever. Only the ~15 query shapes Coroot actually emits are recognised. Anything else
fails **loud** — logged and returned as a ClickHouse exception — so a Coroot upgrade that changes a query trips the
wire instead of silently returning wrong data.

## Author

<div align="center">

**~ Yumiko Sturluson ~**

*Software Engineer*

*コードよ、わがまほうとなれ*

</div>

---

<div align="center">

*~ Built with mass amounts of coffee ~*

</div>
