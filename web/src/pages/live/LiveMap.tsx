import { useEffect, useRef } from 'react'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import type { TrackPoint } from './liveApi'

interface LiveMapProps {
  points: TrackPoint[]
  /** Optional highlighted position (e.g. the replay's current video time). */
  marker?: { lat: number; lon: number } | null
  /** Keep the latest position in view as new points arrive (live mode). */
  follow?: boolean
  className?: string
  ariaLabel?: string
}

// Leaflet map of a GPS track. Uses OpenStreetMap tiles directly — no API key,
// light traffic from a handful of family viewers is well within their usage
// policy. Plain Leaflet (no react-leaflet) keeps the dependency small.
export default function LiveMap({ points, marker, follow = false, className = '', ariaLabel }: LiveMapProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const mapRef = useRef<L.Map | null>(null)
  const lineRef = useRef<L.Polyline | null>(null)
  const headRef = useRef<L.CircleMarker | null>(null)
  const markerRef = useRef<L.CircleMarker | null>(null)
  const fittedRef = useRef(false)
  // Once the viewer pans/zooms themselves, stop auto-following.
  const userMovedRef = useRef(false)

  useEffect(() => {
    if (!containerRef.current || mapRef.current) return
    const map = L.map(containerRef.current, { zoomControl: true, attributionControl: true })
    L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
      maxZoom: 19,
      attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
      className: 'live-map-tiles',
    }).addTo(map)
    map.setView([59.91, 10.75], 12)
    lineRef.current = L.polyline([], { color: '#ef4444', weight: 4, opacity: 0.9 }).addTo(map)
    map.on('dragstart zoomstart', (e: L.LeafletEvent) => {
      // zoomstart also fires for our own setView/fitBounds; only count
      // gestures (they carry an originalEvent).
      if ((e as L.LeafletEvent & { originalEvent?: Event }).originalEvent || e.type === 'dragstart') {
        userMovedRef.current = true
      }
    })
    mapRef.current = map
    // The container may have been laid out after mount (flex/grid).
    const t = setTimeout(() => map.invalidateSize(), 100)
    return () => {
      clearTimeout(t)
      map.remove()
      mapRef.current = null
      lineRef.current = null
      headRef.current = null
      markerRef.current = null
      fittedRef.current = false
    }
  }, [])

  useEffect(() => {
    const map = mapRef.current
    const line = lineRef.current
    if (!map || !line) return
    const latlngs = points.map(p => L.latLng(p.lat, p.lon))
    line.setLatLngs(latlngs)
    if (latlngs.length === 0) return
    const last = latlngs[latlngs.length - 1]
    if (!headRef.current) {
      headRef.current = L.circleMarker(last, { radius: 7, color: '#fff', weight: 2, fillColor: '#ef4444', fillOpacity: 1 }).addTo(map)
    } else {
      headRef.current.setLatLng(last)
    }
    if (!fittedRef.current) {
      fittedRef.current = true
      if (latlngs.length > 1) map.fitBounds(line.getBounds(), { padding: [24, 24], maxZoom: 16 })
      else map.setView(last, 16)
    } else if (follow && !userMovedRef.current) {
      map.panTo(last, { animate: true })
    }
  }, [points, follow])

  useEffect(() => {
    const map = mapRef.current
    if (!map) return
    if (!marker) {
      markerRef.current?.remove()
      markerRef.current = null
      return
    }
    if (!markerRef.current) {
      markerRef.current = L.circleMarker([marker.lat, marker.lon], {
        radius: 8, color: '#fff', weight: 2, fillColor: '#3b82f6', fillOpacity: 1,
      }).addTo(map)
    } else {
      markerRef.current.setLatLng([marker.lat, marker.lon])
    }
  }, [marker])

  return (
    <div
      ref={containerRef}
      role="region"
      aria-label={ariaLabel}
      className={`relative z-0 overflow-hidden rounded-xl bg-gray-800 ${className}`}
    />
  )
}
