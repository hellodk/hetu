'use client'

import { X } from 'lucide-react'
import { useEffect, useCallback, useRef, useId } from 'react'
import clsx from 'clsx'

interface ModalProps {
  isOpen: boolean
  onClose: () => void
  title: string
  children: React.ReactNode
  size?: 'sm' | 'md' | 'lg' | 'xl'
}

const sizeClasses = {
  sm: 'max-w-md',
  md: 'max-w-lg',
  lg: 'max-w-2xl',
  xl: 'max-w-4xl',
}

// Get all focusable elements within a container
function getFocusableElements(container: HTMLElement): HTMLElement[] {
  const elements = container.querySelectorAll(
    'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
  )
  return Array.from(elements) as HTMLElement[]
}

export function Modal({ isOpen, onClose, title, children, size = 'md' }: ModalProps) {
  const modalRef = useRef<HTMLDivElement>(null)
  const previousActiveElement = useRef<HTMLElement | null>(null)
  const closeButtonRef = useRef<HTMLButtonElement>(null)
  const titleId = useId()

  // Handle keyboard events for accessibility
  const handleKeyDown = useCallback((e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      onClose()
      return
    }

    // Focus trap - Tab key handling
    if (e.key === 'Tab' && modalRef.current) {
      const focusableElements = getFocusableElements(modalRef.current)
      if (focusableElements.length === 0) return

      const firstElement = focusableElements[0]
      const lastElement = focusableElements[focusableElements.length - 1]

      if (e.shiftKey) {
        // Shift + Tab
        if (document.activeElement === firstElement) {
          e.preventDefault()
          lastElement.focus()
        }
      } else {
        // Tab
        if (document.activeElement === lastElement) {
          e.preventDefault()
          firstElement.focus()
        }
      }
    }
  }, [onClose])

  useEffect(() => {
    if (isOpen) {
      // Store the previously focused element
      previousActiveElement.current = document.activeElement as HTMLElement

      // Add keyboard listener
      document.addEventListener('keydown', handleKeyDown)

      // Prevent body scroll
      document.body.style.overflow = 'hidden'

      // Focus the close button after a brief delay (for animation)
      setTimeout(() => {
        closeButtonRef.current?.focus()
      }, 100)
    }

    return () => {
      document.removeEventListener('keydown', handleKeyDown)
      document.body.style.overflow = 'unset'

      // Restore focus to the previously focused element
      if (previousActiveElement.current && isOpen) {
        previousActiveElement.current.focus()
      }
    }
  }, [isOpen, handleKeyDown])

  if (!isOpen) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
    >
      {/* Backdrop with animation */}
      <div
        className="modal-backdrop"
        onClick={onClose}
        aria-hidden="true"
      />

      {/* Modal Content with animation */}
      <div
        ref={modalRef}
        className={clsx(
          'modal-content relative w-full bg-cluster-card border border-cluster-border rounded-xl shadow-2xl',
          sizeClasses[size]
        )}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-4 sm:px-6 py-4 border-b border-cluster-border">
          <h2 id={titleId} className="text-lg font-semibold text-cluster-text pr-8">
            {title}
          </h2>
          <button
            ref={closeButtonRef}
            onClick={onClose}
            className="btn-icon absolute right-3 top-3"
            aria-label="Close modal"
          >
            <X className="w-5 h-5 text-cluster-muted" aria-hidden="true" />
          </button>
        </div>

        {/* Body */}
        <div className="p-4 sm:p-6 max-h-[70vh] overflow-y-auto">
          {children}
        </div>
      </div>
    </div>
  )
}
