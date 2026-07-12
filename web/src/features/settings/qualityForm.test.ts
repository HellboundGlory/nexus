import { describe, it, expect } from "vitest"
import {
  buildProfileItems, buildProfilePayload, resolveCutoff, isProfileFormValid,
  defaultNewProfile, formStateFromProfile,
} from "./qualityForm"
import type { QualityDefinition, QualityProfile } from "./qualityTypes"

const defs: QualityDefinition[] = [
  { id: 0, name: "Unknown", source: 0, resolution: 0, rank: 0 },
  { id: 6, name: "WEBDL-720p", source: 6, resolution: 2, rank: 1 },
  { id: 7, name: "WEBDL-1080p", source: 6, resolution: 3, rank: 2 },
]

describe("qualityForm", () => {
  it("builds one item per definition in definition order", () => {
    const items = buildProfileItems({ 7: true }, defs)
    expect(items).toEqual([
      { qualityId: 0, allowed: false },
      { qualityId: 6, allowed: false },
      { qualityId: 7, allowed: true },
    ])
  })

  it("builds a payload from form state", () => {
    const payload = buildProfilePayload(
      { name: "HD", allowed: { 6: true, 7: true }, cutoffQualityId: 7, upgradeAllowed: true },
      defs,
    )
    expect(payload).toEqual({
      name: "HD", cutoffQualityId: 7, upgradeAllowed: true,
      items: [
        { qualityId: 0, allowed: false },
        { qualityId: 6, allowed: true },
        { qualityId: 7, allowed: true },
      ],
    })
  })

  it("keeps a still-allowed cutoff", () => {
    expect(resolveCutoff({ 6: true, 7: true }, 7, defs)).toBe(7)
  })

  it("moves cutoff to the highest allowed when current is disallowed", () => {
    expect(resolveCutoff({ 6: true }, 7, defs)).toBe(6)
  })

  it("returns 0 cutoff when nothing is allowed", () => {
    expect(resolveCutoff({}, 7, defs)).toBe(0)
  })

  it("validates name, at-least-one-allowed, cutoff-allowed", () => {
    expect(isProfileFormValid({ name: "x", allowed: { 7: true }, cutoffQualityId: 7, upgradeAllowed: false })).toBe(true)
    expect(isProfileFormValid({ name: " ", allowed: { 7: true }, cutoffQualityId: 7, upgradeAllowed: false })).toBe(false)
    expect(isProfileFormValid({ name: "x", allowed: {}, cutoffQualityId: 0, upgradeAllowed: false })).toBe(false)
    expect(isProfileFormValid({ name: "x", allowed: { 6: true }, cutoffQualityId: 7, upgradeAllowed: false })).toBe(false)
  })

  it("round-trips a profile into form state (allowed map + cutoff)", () => {
    const p: QualityProfile = {
      id: 1, name: "HD", cutoffQualityId: 7, upgradeAllowed: true, createdAt: "",
      items: [{ qualityId: 6, allowed: true }, { qualityId: 7, allowed: true }, { qualityId: 0, allowed: false }],
    }
    const fs = formStateFromProfile(p, defs)
    expect(fs.name).toBe("HD")
    expect(fs.allowed[7]).toBe(true)
    expect(fs.allowed[0]).toBeFalsy()
    expect(fs.cutoffQualityId).toBe(7)
    expect(fs.upgradeAllowed).toBe(true)
  })

  it("default new profile has a valid quality selection but an empty name", () => {
    const d = defaultNewProfile(defs)
    expect(d.name).toBe("")
    expect(isProfileFormValid(d)).toBe(false) // empty name
    expect(isProfileFormValid({ ...d, name: "HD" })).toBe(true) // allowed+cutoff are valid
  })

  it("default new profile allows 480p/720p/1080p but not 2160p, cutting off at the highest allowed", () => {
    const ladder: QualityDefinition[] = [
      { id: 1, name: "SDTV", source: 4, resolution: 1, rank: 1 },
      { id: 5, name: "HDTV-720p", source: 4, resolution: 2, rank: 2 },
      { id: 7, name: "WEBDL-1080p", source: 6, resolution: 3, rank: 3 },
      { id: 12, name: "Bluray-2160p", source: 7, resolution: 4, rank: 4 },
    ]
    const d = defaultNewProfile(ladder)
    expect(d.allowed[1]).toBe(true)
    expect(d.allowed[5]).toBe(true)
    expect(d.allowed[7]).toBe(true)
    expect(d.allowed[12]).toBeFalsy()
    expect(d.cutoffQualityId).toBe(7)
  })
})
