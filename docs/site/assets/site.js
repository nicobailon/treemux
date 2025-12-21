const wireCopyButtons = () => {
  const buttons = document.querySelectorAll('[data-copy]')
  for (const button of buttons) {
    button.addEventListener('click', async () => {
      const selector = button.getAttribute('data-copy')
      const target = selector ? document.querySelector(selector) : null
      const text = target?.textContent?.trim() ?? ''
      if (!text) return
      try {
        await navigator.clipboard.writeText(text)
        const span = button.querySelector('span')
        const prev = span?.textContent ?? ''
        if (span) span.textContent = 'Copied!'
        window.setTimeout(() => {
          if (span) span.textContent = prev
        }, 1500)
      } catch {
        // ignore
      }
    })
  }
}

const reveal = () => {
  const observer = new IntersectionObserver((entries) => {
    entries.forEach((entry) => {
      if (entry.isIntersecting) {
        entry.target.classList.add('is-on')
      }
    })
  }, { threshold: 0.1 })

  document.querySelectorAll('.reveal').forEach((el) => {
    observer.observe(el)
  })
}

wireCopyButtons()
reveal()
