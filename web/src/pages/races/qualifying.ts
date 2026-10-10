// Marathon qualifying / guaranteed-entry standards, researched from the
// organizers' pages on 2026-10-10. `seconds` is the cutoff time;
// `note` is the short, decision-relevant summary shown on the Season tab.
// Re-research before relying on these for a new edition.
import type { QualifyingStandard } from './season'
import type { Lang } from './racesApi'

export type StandardWithNote = QualifyingStandard & { slug: string; note: Record<Lang, string> }

export const QUALIFYING_RESEARCHED_AT = '2026-10-10'

export const QUALIFYING_STANDARDS: StandardWithNote[] = [
  {
    "race": "boston",
    "name": "Boston Marathon",
    "edition": 2027,
    "kind": "qualifier",
    "eligibility": "International runners eligible (USATF or foreign-equivalent certified course, chip time). Qualifying does NOT guarantee entry: the cutoff below standard was 5:17 for 2027 (2026 4:34, 2025 6:51, 2024 5:29, 2023 0:00, 2022 0:00, 2021 7:47, 2020 1:39). For 2027, 1,000 places also went by random draw to applicants who beat their standard but missed the cutoff. Standards were tightened by 5 min (groups under 60) for 2026 and are unchanged for 2027. New from the 2027 window: net-downhill courses (>1,500 ft drop) get a time index penalty. 2027 registration (14-18 Sep 2026) is closed. Minimum age 18 on race day.",
    "age_rule": "age on race day",
    "window": "Opened 13 Sep 2025, through 2027 registration week (14-18 Sep 2026)",
    "source": "https://www.baa.org/races/boston-marathon/qualify",
    "verified": true,
    "groups": [
      {
        "sex": "male",
        "min_age": 18,
        "max_age": 34,
        "seconds": 10500
      },
      {
        "sex": "female",
        "min_age": 18,
        "max_age": 34,
        "seconds": 12300
      },
      {
        "sex": "nonbinary",
        "min_age": 18,
        "max_age": 34,
        "seconds": 12300
      },
      {
        "sex": "male",
        "min_age": 35,
        "max_age": 39,
        "seconds": 10800
      },
      {
        "sex": "female",
        "min_age": 35,
        "max_age": 39,
        "seconds": 12600
      },
      {
        "sex": "nonbinary",
        "min_age": 35,
        "max_age": 39,
        "seconds": 12600
      },
      {
        "sex": "male",
        "min_age": 40,
        "max_age": 44,
        "seconds": 11100
      },
      {
        "sex": "female",
        "min_age": 40,
        "max_age": 44,
        "seconds": 12900
      },
      {
        "sex": "nonbinary",
        "min_age": 40,
        "max_age": 44,
        "seconds": 12900
      },
      {
        "sex": "male",
        "min_age": 45,
        "max_age": 49,
        "seconds": 11700
      },
      {
        "sex": "female",
        "min_age": 45,
        "max_age": 49,
        "seconds": 13500
      },
      {
        "sex": "nonbinary",
        "min_age": 45,
        "max_age": 49,
        "seconds": 13500
      },
      {
        "sex": "male",
        "min_age": 50,
        "max_age": 54,
        "seconds": 12000
      },
      {
        "sex": "female",
        "min_age": 50,
        "max_age": 54,
        "seconds": 13800
      },
      {
        "sex": "nonbinary",
        "min_age": 50,
        "max_age": 54,
        "seconds": 13800
      },
      {
        "sex": "male",
        "min_age": 55,
        "max_age": 59,
        "seconds": 12600
      },
      {
        "sex": "female",
        "min_age": 55,
        "max_age": 59,
        "seconds": 14400
      },
      {
        "sex": "nonbinary",
        "min_age": 55,
        "max_age": 59,
        "seconds": 14400
      },
      {
        "sex": "male",
        "min_age": 60,
        "max_age": 64,
        "seconds": 13800
      },
      {
        "sex": "female",
        "min_age": 60,
        "max_age": 64,
        "seconds": 15600
      },
      {
        "sex": "nonbinary",
        "min_age": 60,
        "max_age": 64,
        "seconds": 15600
      },
      {
        "sex": "male",
        "min_age": 65,
        "max_age": 69,
        "seconds": 14700
      },
      {
        "sex": "female",
        "min_age": 65,
        "max_age": 69,
        "seconds": 16500
      },
      {
        "sex": "nonbinary",
        "min_age": 65,
        "max_age": 69,
        "seconds": 16500
      },
      {
        "sex": "male",
        "min_age": 70,
        "max_age": 74,
        "seconds": 15600
      },
      {
        "sex": "female",
        "min_age": 70,
        "max_age": 74,
        "seconds": 17400
      },
      {
        "sex": "nonbinary",
        "min_age": 70,
        "max_age": 74,
        "seconds": 17400
      },
      {
        "sex": "male",
        "min_age": 75,
        "max_age": 79,
        "seconds": 16500
      },
      {
        "sex": "female",
        "min_age": 75,
        "max_age": 79,
        "seconds": 18300
      },
      {
        "sex": "nonbinary",
        "min_age": 75,
        "max_age": 79,
        "seconds": 18300
      },
      {
        "sex": "male",
        "min_age": 80,
        "max_age": 99,
        "seconds": 17400
      },
      {
        "sex": "female",
        "min_age": 80,
        "max_age": 99,
        "seconds": 19200
      },
      {
        "sex": "nonbinary",
        "min_age": 80,
        "max_age": 99,
        "seconds": 19200
      }
    ],
    "race_date": "2027-04-19",
    "slug": "boston-marathon-2027",
    "note": {
      "en": "Meeting the standard does not guarantee entry: for 2027 you had to be 5:17 under it. International runners are eligible.",
      "nb": "Å klare kravet gir ikke garantert plass: for 2027 måtte du være 5:17 under. Utenlandske løpere kan søke.",
      "th": "ผ่านเกณฑ์ไม่ได้รับประกันสิทธิ์: ปี 2027 ต้องเร็วกว่าเกณฑ์ 5:17 นักวิ่งต่างชาติสมัครได้"
    }
  },
  {
    "race": "berlin",
    "name": "BMW Berlin Marathon",
    "edition": 2027,
    "kind": "guaranteed_entry",
    "eligibility": "'Fast runner' registration is open to international runners. Marathon times only, from 2025 or 2026, at AIMS-certified, USATF-listed or WMM age-group-ranking races, with an official certificate or results link as proof. Despite the 'guaranteed' label, the organizer says fast-runner registration does not guarantee a place: proofs are reviewed and acceptance is announced with the lottery results (rejected proofs go into the lottery). The organizer site is now generali-berlin-marathon.com. No non-binary standard published.",
    "age_rule": "by birth year (age reached in calendar year 2027): 2009-1983 = 18-44, 1982-1968 = 45-59, 1967 or older = 60+",
    "window": "Marathons run in 2025 or 2026 (not older than 2 years); apply during the 2027 lottery period (late Sep - early Nov 2026)",
    "source": "https://www.generali-berlin-marathon.com/en/registration/run",
    "verified": true,
    "groups": [
      {
        "sex": "male",
        "min_age": 18,
        "max_age": 44,
        "seconds": 9900
      },
      {
        "sex": "female",
        "min_age": 18,
        "max_age": 44,
        "seconds": 11400
      },
      {
        "sex": "male",
        "min_age": 45,
        "max_age": 59,
        "seconds": 10500
      },
      {
        "sex": "female",
        "min_age": 45,
        "max_age": 59,
        "seconds": 12600
      },
      {
        "sex": "male",
        "min_age": 60,
        "max_age": 99,
        "seconds": 12300
      },
      {
        "sex": "female",
        "min_age": 60,
        "max_age": 99,
        "seconds": 15600
      }
    ],
    "race_date": "2027-09-26",
    "slug": "bmw-berlin-marathon-2027",
    "note": {
      "en": "Fast-runner entry for international runners; times from 2025 or 2026 only, checked before a place is given. Age bands go by birth year.",
      "nb": "Hurtigløperplass for utenlandske løpere; bare tider fra 2025 eller 2026, og de sjekkes før du får plass. Aldersgruppen følger fødselsår.",
      "th": "สิทธิ์นักวิ่งเร็วสำหรับชาวต่างชาติ ใช้เวลาเฉพาะปี 2025 หรือ 2026 และตรวจสอบก่อนให้สิทธิ์ กลุ่มอายุนับตามปีเกิด"
    }
  },
  {
    "race": "london",
    "name": "TCS London Marathon",
    "edition": 2027,
    "kind": "qualifier",
    "eligibility": "Good For Age is for UK residents ONLY; international runners are not eligible (they must use the ballot, a charity or a tour operator). Times are 'sub' (must be faster than) the listed time, on a UKA/AIMS/national-body certified course. Capped at 6,000 places (3,000 men, 3,000 women), allocated fastest-first relative to the age standard; meeting the time only allows an application. 2027 is the two-day 'Double' (men's GFA Sat 24 Apr, women's Sun 25 Apr). Apply by 16:00 GMT 29 Oct 2026. No non-binary GFA standard published. No separate Championship-entry standard on the page.",
    "age_rule": "age when the qualifying time was run",
    "window": "1 Oct 2025 - 30 Sep 2026",
    "source": "https://www.londonmarathonevents.co.uk/london-marathon/good-age-entry",
    "verified": true,
    "groups": [
      {
        "sex": "male",
        "min_age": 18,
        "max_age": 39,
        "seconds": 10320
      },
      {
        "sex": "female",
        "min_age": 18,
        "max_age": 39,
        "seconds": 13080
      },
      {
        "sex": "male",
        "min_age": 40,
        "max_age": 44,
        "seconds": 10620
      },
      {
        "sex": "female",
        "min_age": 40,
        "max_age": 44,
        "seconds": 13380
      },
      {
        "sex": "male",
        "min_age": 45,
        "max_age": 49,
        "seconds": 10920
      },
      {
        "sex": "female",
        "min_age": 45,
        "max_age": 49,
        "seconds": 13560
      },
      {
        "sex": "male",
        "min_age": 50,
        "max_age": 54,
        "seconds": 11220
      },
      {
        "sex": "female",
        "min_age": 50,
        "max_age": 54,
        "seconds": 13980
      },
      {
        "sex": "male",
        "min_age": 55,
        "max_age": 59,
        "seconds": 11520
      },
      {
        "sex": "female",
        "min_age": 55,
        "max_age": 59,
        "seconds": 14280
      },
      {
        "sex": "male",
        "min_age": 60,
        "max_age": 64,
        "seconds": 12840
      },
      {
        "sex": "female",
        "min_age": 60,
        "max_age": 64,
        "seconds": 15780
      },
      {
        "sex": "male",
        "min_age": 65,
        "max_age": 69,
        "seconds": 13920
      },
      {
        "sex": "female",
        "min_age": 65,
        "max_age": 69,
        "seconds": 17580
      },
      {
        "sex": "male",
        "min_age": 70,
        "max_age": 74,
        "seconds": 17520
      },
      {
        "sex": "female",
        "min_age": 70,
        "max_age": 74,
        "seconds": 21180
      },
      {
        "sex": "male",
        "min_age": 75,
        "max_age": 79,
        "seconds": 18420
      },
      {
        "sex": "female",
        "min_age": 75,
        "max_age": 79,
        "seconds": 22380
      },
      {
        "sex": "male",
        "min_age": 80,
        "max_age": 84,
        "seconds": 19620
      },
      {
        "sex": "female",
        "min_age": 80,
        "max_age": 84,
        "seconds": 23880
      },
      {
        "sex": "male",
        "min_age": 85,
        "max_age": 89,
        "seconds": 22200
      },
      {
        "sex": "female",
        "min_age": 85,
        "max_age": 89,
        "seconds": 25800
      },
      {
        "sex": "male",
        "min_age": 90,
        "max_age": 99,
        "seconds": 26400
      },
      {
        "sex": "female",
        "min_age": 90,
        "max_age": 99,
        "seconds": 27900
      }
    ],
    "race_date": "2027-04-25",
    "slug": "tcs-london-marathon-2027",
    "note": {
      "en": "UK residents only — international runners must use the ballot, a charity or a tour operator. Age counts when the time was run.",
      "nb": "Bare for bosatte i Storbritannia — utenlandske løpere må bruke trekning, veldedighet eller reisebyrå. Alderen gjelder da tiden ble løpt.",
      "th": "เฉพาะผู้พำนักในสหราชอาณาจักร นักวิ่งต่างชาติต้องใช้การจับฉลาก การกุศล หรือบริษัททัวร์"
    }
  },
  {
    "race": "chicago",
    "name": "Bank of America Chicago Marathon",
    "edition": 2027,
    "kind": "guaranteed_entry",
    "eligibility": "Time-qualifier guaranteed entry. No residency restriction stated (same application for all). Full-marathon times on a certified course (USATF, World Athletics or equivalent) only. Application window 8 Oct - 29 Oct 2026 (2 pm CT). Race 10 Oct 2027.",
    "age_rule": "age on race day (10 Oct 2027)",
    "window": "1 Jan 2025 - 29 Oct 2026",
    "source": "https://www.chicagomarathon.com/apply/",
    "verified": true,
    "groups": [
      {
        "sex": "male",
        "min_age": 16,
        "max_age": 34,
        "seconds": 10200
      },
      {
        "sex": "female",
        "min_age": 16,
        "max_age": 34,
        "seconds": 12000
      },
      {
        "sex": "nonbinary",
        "min_age": 16,
        "max_age": 34,
        "seconds": 12000
      },
      {
        "sex": "male",
        "min_age": 35,
        "max_age": 39,
        "seconds": 10500
      },
      {
        "sex": "female",
        "min_age": 35,
        "max_age": 39,
        "seconds": 12300
      },
      {
        "sex": "nonbinary",
        "min_age": 35,
        "max_age": 39,
        "seconds": 12300
      },
      {
        "sex": "male",
        "min_age": 40,
        "max_age": 44,
        "seconds": 10800
      },
      {
        "sex": "female",
        "min_age": 40,
        "max_age": 44,
        "seconds": 12600
      },
      {
        "sex": "nonbinary",
        "min_age": 40,
        "max_age": 44,
        "seconds": 12600
      },
      {
        "sex": "male",
        "min_age": 45,
        "max_age": 49,
        "seconds": 11400
      },
      {
        "sex": "female",
        "min_age": 45,
        "max_age": 49,
        "seconds": 13200
      },
      {
        "sex": "nonbinary",
        "min_age": 45,
        "max_age": 49,
        "seconds": 13200
      },
      {
        "sex": "male",
        "min_age": 50,
        "max_age": 54,
        "seconds": 11700
      },
      {
        "sex": "female",
        "min_age": 50,
        "max_age": 54,
        "seconds": 13800
      },
      {
        "sex": "nonbinary",
        "min_age": 50,
        "max_age": 54,
        "seconds": 13800
      },
      {
        "sex": "male",
        "min_age": 55,
        "max_age": 59,
        "seconds": 12300
      },
      {
        "sex": "female",
        "min_age": 55,
        "max_age": 59,
        "seconds": 14100
      },
      {
        "sex": "nonbinary",
        "min_age": 55,
        "max_age": 59,
        "seconds": 14100
      },
      {
        "sex": "male",
        "min_age": 60,
        "max_age": 64,
        "seconds": 13200
      },
      {
        "sex": "female",
        "min_age": 60,
        "max_age": 64,
        "seconds": 15300
      },
      {
        "sex": "nonbinary",
        "min_age": 60,
        "max_age": 64,
        "seconds": 15300
      },
      {
        "sex": "male",
        "min_age": 65,
        "max_age": 69,
        "seconds": 14100
      },
      {
        "sex": "female",
        "min_age": 65,
        "max_age": 69,
        "seconds": 16200
      },
      {
        "sex": "nonbinary",
        "min_age": 65,
        "max_age": 69,
        "seconds": 16200
      },
      {
        "sex": "male",
        "min_age": 70,
        "max_age": 74,
        "seconds": 15300
      },
      {
        "sex": "female",
        "min_age": 70,
        "max_age": 74,
        "seconds": 17100
      },
      {
        "sex": "nonbinary",
        "min_age": 70,
        "max_age": 74,
        "seconds": 17100
      },
      {
        "sex": "male",
        "min_age": 75,
        "max_age": 79,
        "seconds": 16200
      },
      {
        "sex": "female",
        "min_age": 75,
        "max_age": 79,
        "seconds": 18000
      },
      {
        "sex": "nonbinary",
        "min_age": 75,
        "max_age": 79,
        "seconds": 18000
      },
      {
        "sex": "male",
        "min_age": 80,
        "max_age": 99,
        "seconds": 17400
      },
      {
        "sex": "female",
        "min_age": 80,
        "max_age": 99,
        "seconds": 19200
      },
      {
        "sex": "nonbinary",
        "min_age": 80,
        "max_age": 99,
        "seconds": 19200
      }
    ],
    "race_date": "2027-10-10",
    "slug": "bank-of-america-chicago-marathon-2027",
    "note": {
      "en": "Guaranteed entry, no residency limit. Window 1 Jan 2025 – 29 Oct 2026; apply by 29 Oct 2026.",
      "nb": "Garantert plass uten krav om bosted. Tider fra 1. jan 2025 til 29. okt 2026; søk innen 29. okt 2026.",
      "th": "รับประกันสิทธิ์ ไม่จำกัดถิ่นที่อยู่ ใช้เวลาตั้งแต่ 1 ม.ค. 2025 ถึง 29 ต.ค. 2026 สมัครภายใน 29 ต.ค. 2026"
    }
  },
  {
    "race": "nyc",
    "name": "TCS New York City Marathon",
    "edition": 2027,
    "kind": "qualifier",
    "eligibility": "Two routes. (a) NYRR TQ: guaranteed entry for standards met at listed 2026 NYRR races (NYC Half, Women's Half, Brooklyn Half, Staten Island Half, NYC Marathon); half-marathon standards apply there and are not included here. (b) Non-NYRR TQ: open to any runner including international, certified full marathon only, limited places, NOT guaranteed; the fastest per age/gender are accepted if oversubscribed (reportedly ~10% accepted for 2026). The values here are the full-marathon standards. Application dates TBD.",
    "age_rule": "age on race day (7 Nov 2027)",
    "window": "1 Jan 2026 - 31 Dec 2026",
    "source": "https://www.nyrr.org/tcsnycmarathon/time-qualifiers",
    "verified": true,
    "groups": [
      {
        "sex": "male",
        "min_age": 18,
        "max_age": 34,
        "seconds": 10380
      },
      {
        "sex": "female",
        "min_age": 18,
        "max_age": 34,
        "seconds": 11580
      },
      {
        "sex": "nonbinary",
        "min_age": 18,
        "max_age": 34,
        "seconds": 11580
      },
      {
        "sex": "male",
        "min_age": 35,
        "max_age": 39,
        "seconds": 10500
      },
      {
        "sex": "female",
        "min_age": 35,
        "max_age": 39,
        "seconds": 11700
      },
      {
        "sex": "nonbinary",
        "min_age": 35,
        "max_age": 39,
        "seconds": 11700
      },
      {
        "sex": "male",
        "min_age": 40,
        "max_age": 44,
        "seconds": 10680
      },
      {
        "sex": "female",
        "min_age": 40,
        "max_age": 44,
        "seconds": 12360
      },
      {
        "sex": "nonbinary",
        "min_age": 40,
        "max_age": 44,
        "seconds": 12360
      },
      {
        "sex": "male",
        "min_age": 45,
        "max_age": 49,
        "seconds": 11100
      },
      {
        "sex": "female",
        "min_age": 45,
        "max_age": 49,
        "seconds": 13080
      },
      {
        "sex": "nonbinary",
        "min_age": 45,
        "max_age": 49,
        "seconds": 13080
      },
      {
        "sex": "male",
        "min_age": 50,
        "max_age": 54,
        "seconds": 11640
      },
      {
        "sex": "female",
        "min_age": 50,
        "max_age": 54,
        "seconds": 13860
      },
      {
        "sex": "nonbinary",
        "min_age": 50,
        "max_age": 54,
        "seconds": 13860
      },
      {
        "sex": "male",
        "min_age": 55,
        "max_age": 59,
        "seconds": 12180
      },
      {
        "sex": "female",
        "min_age": 55,
        "max_age": 59,
        "seconds": 15000
      },
      {
        "sex": "nonbinary",
        "min_age": 55,
        "max_age": 59,
        "seconds": 15000
      },
      {
        "sex": "male",
        "min_age": 60,
        "max_age": 64,
        "seconds": 12840
      },
      {
        "sex": "female",
        "min_age": 60,
        "max_age": 64,
        "seconds": 16020
      },
      {
        "sex": "nonbinary",
        "min_age": 60,
        "max_age": 64,
        "seconds": 16020
      },
      {
        "sex": "male",
        "min_age": 65,
        "max_age": 69,
        "seconds": 13500
      },
      {
        "sex": "female",
        "min_age": 65,
        "max_age": 69,
        "seconds": 17400
      },
      {
        "sex": "nonbinary",
        "min_age": 65,
        "max_age": 69,
        "seconds": 17400
      },
      {
        "sex": "male",
        "min_age": 70,
        "max_age": 74,
        "seconds": 15000
      },
      {
        "sex": "female",
        "min_age": 70,
        "max_age": 74,
        "seconds": 19800
      },
      {
        "sex": "nonbinary",
        "min_age": 70,
        "max_age": 74,
        "seconds": 19800
      },
      {
        "sex": "male",
        "min_age": 75,
        "max_age": 79,
        "seconds": 16200
      },
      {
        "sex": "female",
        "min_age": 75,
        "max_age": 79,
        "seconds": 21600
      },
      {
        "sex": "nonbinary",
        "min_age": 75,
        "max_age": 79,
        "seconds": 21600
      },
      {
        "sex": "male",
        "min_age": 80,
        "max_age": 99,
        "seconds": 17700
      },
      {
        "sex": "female",
        "min_age": 80,
        "max_age": 99,
        "seconds": 23700
      },
      {
        "sex": "nonbinary",
        "min_age": 80,
        "max_age": 99,
        "seconds": 23700
      }
    ],
    "race_date": "2027-11-07",
    "slug": "tcs-new-york-city-marathon-2027",
    "note": {
      "en": "Times from 2026. Non-NYRR races give limited, non-guaranteed places — the fastest per group get in if oversubscribed.",
      "nb": "Tider fra 2026. Løp utenfor NYRR gir et begrenset antall plasser uten garanti — de raskeste i hver gruppe får plass ved for mange søkere.",
      "th": "ใช้เวลาปี 2026 สนามนอก NYRR มีสิทธิ์จำกัดและไม่รับประกัน หากผู้สมัครเกิน ผู้ที่เร็วที่สุดในแต่ละกลุ่มได้สิทธิ์"
    }
  },
  {
    "race": "tokyo",
    "name": "Tokyo Marathon",
    "edition": 2027,
    "kind": "qualifier",
    "eligibility": "No age-group time qualifier for amateurs. The only route is 'RUN as ONE - Semi-Elite (Overseas)' for non-Japan residents: men under 2:22:00, women under 2:50:00 (gun time) at a World Athletics Label road race. 25 places per gender, fastest accepted, not guaranteed. Application 31 Jul - 13 Aug 2026 (closed). The single open standard is stored as one 18-99 group per sex.",
    "age_rule": "none (single open standard)",
    "window": "Jul 2024 - Jun 2026",
    "source": "https://www.marathon.tokyo/en/participants/run-as-one/",
    "verified": true,
    "groups": [
      {
        "sex": "male",
        "min_age": 18,
        "max_age": 99,
        "seconds": 8520
      },
      {
        "sex": "female",
        "min_age": 18,
        "max_age": 99,
        "seconds": 10200
      }
    ],
    "race_date": "2027-03-07",
    "slug": "tokyo-marathon-2027",
    "note": {
      "en": "No age-group qualifier — only the overseas semi-elite programme (25 places per gender, gun time). Closed for 2027.",
      "nb": "Ingen kvalifisering per aldersgruppe — bare semi-elite for utlendinger (25 plasser per kjønn, bruttotid). Stengt for 2027.",
      "th": "ไม่มีเกณฑ์ตามกลุ่มอายุ มีเพียงโครงการกึ่งอีลีทสำหรับชาวต่างชาติ (25 ที่ต่อเพศ) ปิดรับสำหรับปี 2027 แล้ว"
    }
  },
  {
    "race": "sydney",
    "name": "TCS Sydney Marathon",
    "edition": 2027,
    "kind": "qualifier",
    "eligibility": "High Performance Program; international athletes eligible. World Athletics-certified marathon with at most 457 m net drop. 1,200 guaranteed (paid) entries go to those SELECTED: the fastest 600 across categories get Sub-Elite, the next 600 get Good For Age. Meeting the standard alone does not guarantee selection. 2027 applications are closed; results early Nov 2026. Race 29 Aug 2027.",
    "age_rule": "age on race day (29 Aug 2027)",
    "window": "Since 1 Jul 2025 (until applications closed, Oct 2026)",
    "source": "https://www.tcssydneymarathon.com/high-performance-program",
    "verified": true,
    "groups": [
      {
        "sex": "male",
        "min_age": 18,
        "max_age": 34,
        "seconds": 9900
      },
      {
        "sex": "female",
        "min_age": 18,
        "max_age": 34,
        "seconds": 11880
      },
      {
        "sex": "nonbinary",
        "min_age": 18,
        "max_age": 34,
        "seconds": 11880
      },
      {
        "sex": "male",
        "min_age": 35,
        "max_age": 39,
        "seconds": 10020
      },
      {
        "sex": "female",
        "min_age": 35,
        "max_age": 39,
        "seconds": 12000
      },
      {
        "sex": "nonbinary",
        "min_age": 35,
        "max_age": 39,
        "seconds": 12000
      },
      {
        "sex": "male",
        "min_age": 40,
        "max_age": 44,
        "seconds": 10260
      },
      {
        "sex": "female",
        "min_age": 40,
        "max_age": 44,
        "seconds": 12420
      },
      {
        "sex": "nonbinary",
        "min_age": 40,
        "max_age": 44,
        "seconds": 12420
      },
      {
        "sex": "male",
        "min_age": 45,
        "max_age": 49,
        "seconds": 10500
      },
      {
        "sex": "female",
        "min_age": 45,
        "max_age": 49,
        "seconds": 12900
      },
      {
        "sex": "nonbinary",
        "min_age": 45,
        "max_age": 49,
        "seconds": 12900
      },
      {
        "sex": "male",
        "min_age": 50,
        "max_age": 54,
        "seconds": 10800
      },
      {
        "sex": "female",
        "min_age": 50,
        "max_age": 54,
        "seconds": 13380
      },
      {
        "sex": "nonbinary",
        "min_age": 50,
        "max_age": 54,
        "seconds": 13380
      },
      {
        "sex": "male",
        "min_age": 55,
        "max_age": 59,
        "seconds": 11160
      },
      {
        "sex": "female",
        "min_age": 55,
        "max_age": 59,
        "seconds": 13860
      },
      {
        "sex": "nonbinary",
        "min_age": 55,
        "max_age": 59,
        "seconds": 13860
      },
      {
        "sex": "male",
        "min_age": 60,
        "max_age": 64,
        "seconds": 11820
      },
      {
        "sex": "female",
        "min_age": 60,
        "max_age": 64,
        "seconds": 15180
      },
      {
        "sex": "nonbinary",
        "min_age": 60,
        "max_age": 64,
        "seconds": 15180
      },
      {
        "sex": "male",
        "min_age": 65,
        "max_age": 69,
        "seconds": 13260
      },
      {
        "sex": "female",
        "min_age": 65,
        "max_age": 69,
        "seconds": 15840
      },
      {
        "sex": "nonbinary",
        "min_age": 65,
        "max_age": 69,
        "seconds": 15840
      },
      {
        "sex": "male",
        "min_age": 70,
        "max_age": 74,
        "seconds": 14820
      },
      {
        "sex": "female",
        "min_age": 70,
        "max_age": 74,
        "seconds": 16500
      },
      {
        "sex": "nonbinary",
        "min_age": 70,
        "max_age": 74,
        "seconds": 16500
      },
      {
        "sex": "male",
        "min_age": 75,
        "max_age": 79,
        "seconds": 16980
      },
      {
        "sex": "female",
        "min_age": 75,
        "max_age": 79,
        "seconds": 19800
      },
      {
        "sex": "nonbinary",
        "min_age": 75,
        "max_age": 79,
        "seconds": 19800
      },
      {
        "sex": "male",
        "min_age": 80,
        "max_age": 99,
        "seconds": 20760
      },
      {
        "sex": "female",
        "min_age": 80,
        "max_age": 99,
        "seconds": 23760
      },
      {
        "sex": "nonbinary",
        "min_age": 80,
        "max_age": 99,
        "seconds": 23760
      }
    ],
    "race_date": "2027-08-29",
    "slug": "tcs-sydney-marathon-2027",
    "note": {
      "en": "1,200 places go to the fastest applicants, so the standard alone isn’t a guarantee. Closed for 2027; results in early November.",
      "nb": "1 200 plasser går til de raskeste søkerne, så kravet alene gir ingen garanti. Stengt for 2027; svar tidlig i november.",
      "th": "มี 1,200 ที่ให้ผู้สมัครที่เร็วที่สุด ผ่านเกณฑ์อย่างเดียวจึงไม่รับประกัน ปิดรับปี 2027 ประกาศผลต้นเดือนพฤศจิกายน"
    }
  },
  {
    "race": "valencia",
    "name": "Valencia Marathon",
    "edition": 2026,
    "kind": "qualifier",
    "eligibility": "No age-group or Good For Age entry. Only sub-elite bibs (regulations art. 6.4): men under 2:20:00 marathon (or 1:06:00 half / 30:00 10k), women under 2:45:00 (or 1:17:30 / 35:30), with times from 2024-2026. Sub-elite bibs were sold first-come-first-served from 2 May 2026, outside the ballot, until sold out. 2027 regulations are not published yet; these are the 2026 rules.",
    "age_rule": "none (single open standard)",
    "window": "Performances from 2024, 2025, 2026",
    "source": "https://www.valenciaciudaddelrunning.com/en/marathon/marathon-regulations/",
    "verified": false,
    "groups": [
      {
        "sex": "male",
        "min_age": 18,
        "max_age": 99,
        "seconds": 8400
      },
      {
        "sex": "female",
        "min_age": 18,
        "max_age": 99,
        "seconds": 9900
      }
    ],
    "race_date": "2027-12-05",
    "slug": "maraton-valencia-trinidad-alfonso-zurich-2027",
    "note": {
      "en": "No age-group entry — only sub-elite bibs, sold first come, first served outside the ballot (2026 rules).",
      "nb": "Ingen plass per aldersgruppe — bare sub-elite-startnumre, solgt etter først til mølla utenfor trekningen (2026-regler).",
      "th": "ไม่มีสิทธิ์ตามกลุ่มอายุ มีเพียงบิบซับอีลีทแบบมาก่อนได้ก่อนนอกการจับฉลาก (กติกาปี 2026)"
    }
  },
  {
    "race": "copenhagen",
    "name": "Copenhagen Marathon",
    "edition": 2027,
    "kind": "qualifier",
    "eligibility": "No age-group standards. Only the 'Elite B' field, open outside the lottery to any runner who meets the standard and provides documentation: men 2:25:00 or faster, women 2:50:00 or faster (also half 1:08:30/1:20:00, 10k 31:00/36:00).",
    "age_rule": "none (single open standard)",
    "window": "1 Jan 2026 - 25 Apr 2027",
    "source": "https://copenhagenmarathon.dk/en/my-race/elite/",
    "verified": true,
    "groups": [
      {
        "sex": "male",
        "min_age": 18,
        "max_age": 99,
        "seconds": 8700
      },
      {
        "sex": "female",
        "min_age": 18,
        "max_age": 99,
        "seconds": 10200
      }
    ],
    "race_date": "2027-05-09",
    "slug": "copenhagen-marathon-2027",
    "note": {
      "en": "Elite B field outside the lottery for anyone who meets the standard with documentation; window 1 Jan 2026 – 25 Apr 2027.",
      "nb": "Elite B-feltet utenom trekningen for alle som klarer kravet og dokumenterer det; tider fra 1. jan 2026 til 25. apr 2027.",
      "th": "กลุ่ม Elite B นอกการจับฉลากสำหรับผู้ผ่านเกณฑ์พร้อมหลักฐาน ใช้เวลาตั้งแต่ 1 ม.ค. 2026 ถึง 25 เม.ย. 2027"
    }
  },
  {
    "race": "stockholm",
    "name": "Stockholm Marathon",
    "edition": 2027,
    "kind": "qualifier",
    "eligibility": "No qualifying or guaranteed-entry time standard (open registration). According to secondary sources, times faster than ~3:35 only need verification for start-group seeding; not checked on the organizer page.",
    "age_rule": "n/a",
    "window": "n/a",
    "source": "https://www.stockholmmarathon.se/",
    "verified": false,
    "groups": [],
    "race_date": "2027-05-29",
    "slug": "adidas-stockholm-marathon-2027",
    "note": {
      "en": "No qualifying time — open registration; a fast time only affects your start group.",
      "nb": "Ingen kvalifiseringstid — åpen påmelding; en rask tid påvirker bare startgruppen.",
      "th": "ไม่มีเกณฑ์เวลา สมัครได้ทั่วไป เวลาที่เร็วมีผลแค่กลุ่มปล่อยตัว"
    }
  }
]
