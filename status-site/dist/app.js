(() => {
  const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  if (reduceMotion || !('IntersectionObserver' in window)) return;

  const items = document.querySelectorAll('.milestone, .principle-grid article');
  items.forEach((item) => {
    item.style.opacity = '0';
    item.style.transform = 'translateY(14px)';
    item.style.transition = 'opacity 450ms ease, transform 450ms ease';
  });

  const observer = new IntersectionObserver((entries) => {
    entries.forEach((entry) => {
      if (!entry.isIntersecting) return;
      entry.target.style.opacity = '1';
      entry.target.style.transform = 'translateY(0)';
      observer.unobserve(entry.target);
    });
  }, { threshold: 0.12 });

  items.forEach((item) => observer.observe(item));
})();
