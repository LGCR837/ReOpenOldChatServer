import React, { useState, useEffect, useRef } from 'react';
import { TileSize } from '../types';

interface LiveTileProps {
  size: TileSize;
  title?: string;
  count?: number | string;
  icon?: React.ReactNode;
  bgColor?: string;
  contentBack?: React.ReactNode;
  onClick?: () => void;
  delay?: number;
  liveEffect?: boolean;
}

const LiveTile: React.FC<LiveTileProps> = ({
  size,
  title,
  count,
  icon,
  bgColor = 'bg-metro-accent',
  contentBack,
  onClick,
  delay = 0,
  liveEffect = true
}) => {
  const [isFlipped, setIsFlipped] = useState(false);
  const [isHovered, setIsHovered] = useState(false);
  const [transformStyle, setTransformStyle] = useState('');
  const [transitionStyle, setTransitionStyle] = useState('');
  
  const tileRef = useRef<HTMLDivElement>(null);
  const mousePosRef = useRef<{x: number, y: number} | null>(null);
  const isPressedRef = useRef(false);

  // Random flip effect logic
  useEffect(() => {
    if (!liveEffect) return;
    const randomInterval = Math.floor(Math.random() * 10000) + 5000;
    const interval = setInterval(() => {
      if (!isHovered) {
        setIsFlipped(prev => !prev);
      }
    }, randomInterval);
    return () => clearInterval(interval);
  }, [liveEffect, isHovered]);

  const getSizeClasses = () => {
    switch (size) {
      case TileSize.SMALL: return 'col-span-1 row-span-1 h-24 sm:h-32';
      case TileSize.MEDIUM: return 'col-span-2 row-span-2 h-48 sm:h-64';
      case TileSize.WIDE: return 'col-span-4 row-span-2 h-48 sm:h-64';
      case TileSize.LARGE: return 'col-span-4 row-span-4 h-96 sm:h-[32rem]';
      default: return 'col-span-2 row-span-2 h-48 sm:h-64';
    }
  };

  const calculateTilt = (x: number, y: number, width: number, height: number, pressed: boolean) => {
    // Calculate percentages (-1 to 1) relative to center
    const xPct = (x / width - 0.5) * 2;
    const yPct = (y / height - 0.5) * 2;

    // Configuration
    // Hover: Tilt active, no scale (keep flat on plane), preventing protrusion
    // Press: Heavy tilt, scale down (depress)
    const MAX_TILT = pressed ? 25 : 10; 
    const SCALE = pressed ? 0.95 : 1.0; 

    const rotX = -yPct * MAX_TILT; 
    const rotY = xPct * MAX_TILT;

    // Protrusion Correction Logic
    // We calculate the Z position of all 4 corners based on the rotation.
    // We find the maximum Z (the corner sticking out the most).
    // We subtract this maxZ via translateZ to ensure the highest point is at Z=0 (plane level).
    const radX = rotX * (Math.PI / 180);
    const radY = rotY * (Math.PI / 180);
    const w2 = width / 2;
    const h2 = height / 2;

    const corners = [
        { x: -w2, y: -h2 }, // Top Left
        { x: w2, y: -h2 },  // Top Right
        { x: -w2, y: h2 },  // Bottom Left
        { x: w2, y: h2 },   // Bottom Right
    ];

    let maxZ = 0;
    for (const c of corners) {
        const z = c.y * Math.sin(radX) - c.x * Math.sin(radY);
        if (z > maxZ) maxZ = z;
    }

    // Apply negative Z translation to neutralize protrusion
    const translateZ = -maxZ;

    return `scale(${SCALE}) perspective(800px) translateZ(${translateZ}px) rotateX(${rotX}deg) rotateY(${rotY}deg)`;
  };

  const handleMouseMove = (e: React.MouseEvent<HTMLDivElement>) => {
    if (!tileRef.current) return;
    
    const rect = tileRef.current.getBoundingClientRect();
    const x = e.clientX - rect.left;
    const y = e.clientY - rect.top;
    
    mousePosRef.current = { x, y };

    // Very fast transition for hover to feel responsive but smooth
    setTransitionStyle('transform 0.1s ease-out'); 
    setTransformStyle(calculateTilt(x, y, rect.width, rect.height, isPressedRef.current));
    setIsHovered(true);
  };

  const handlePointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    if (!tileRef.current) return;
    e.currentTarget.setPointerCapture(e.pointerId);
    isPressedRef.current = true;

    const rect = tileRef.current.getBoundingClientRect();
    const x = e.clientX - rect.left;
    const y = e.clientY - rect.top;
    
    mousePosRef.current = { x, y };

    // Snap quickly on press
    setTransitionStyle('transform 0.05s ease-out');
    setTransformStyle(calculateTilt(x, y, rect.width, rect.height, true));
  };

  const handlePointerUp = (e: React.PointerEvent<HTMLDivElement>) => {
     e.currentTarget.releasePointerCapture(e.pointerId);
     isPressedRef.current = false;
     
     if (onClick) onClick();

     if (mousePosRef.current && tileRef.current) {
        // Recover to hover state if cursor is still inside
        const rect = tileRef.current.getBoundingClientRect();
        // Smooth recovery
        setTransitionStyle('transform 0.3s cubic-bezier(0.25, 0.46, 0.45, 0.94)'); 
        setTransformStyle(calculateTilt(mousePosRef.current.x, mousePosRef.current.y, rect.width, rect.height, false));
     } else {
        handleRelease();
     }
  };

  const handleMouseLeave = () => {
    setIsHovered(false);
    isPressedRef.current = false;
    mousePosRef.current = null;
    handleRelease();
  };

  const handleRelease = () => {
    // Windows Phone style wobble on release
    setTransitionStyle('transform 0.5s cubic-bezier(0.2, 0.8, 0.2, 1.1)'); 
    setTransformStyle('scale(1) perspective(800px) translateZ(0) rotateX(0deg) rotateY(0deg)');
  };

  return (
    <div 
      ref={tileRef}
      className={`relative ${getSizeClasses()} group cursor-pointer select-none`}
      style={{ 
        animationDelay: `${delay}ms`,
        transform: transformStyle,
        transition: transitionStyle,
        zIndex: isHovered ? 20 : 1
      }}
      onMouseEnter={() => setIsHovered(true)}
      onMouseMove={handleMouseMove}
      onMouseLeave={handleMouseLeave}
      onPointerDown={handlePointerDown}
      onPointerUp={handlePointerUp}
    >
      <div 
        className={`w-full h-full relative transform-style-3d transition-transform duration-700 ease-[cubic-bezier(0.68,-0.55,0.265,1.55)] ${isFlipped ? 'rotate-x-180' : ''}`}
      >
        {/* Front Face */}
        <div className={`absolute w-full h-full backface-hidden ${bgColor} p-3 flex flex-col justify-between rounded-none ring-0`}>
          <div className="flex justify-between items-start">
            {icon && <div className="text-white transform transition-transform duration-300 group-hover:scale-90">{icon}</div>}
            {count && <span className="text-xl font-light">{count}</span>}
          </div>
          {title && <span className="text-sm font-semibold tracking-wide uppercase mt-auto">{title}</span>}
          
          {/* Enhanced Shine Effect */}
          <div 
            className="absolute inset-0 bg-gradient-to-br from-white/20 via-transparent to-black/30 opacity-0 group-hover:opacity-100 transition-opacity duration-300 pointer-events-none"
            style={{ mixBlendMode: 'overlay' }}
          />
        </div>

        {/* Back Face */}
        <div className={`absolute w-full h-full backface-hidden rotate-x-180 ${bgColor} p-4 flex flex-col justify-center items-center rounded-none ring-0`}>
           {contentBack ? contentBack : (
             <div className="text-center">
               <span className="text-lg font-light leading-tight">{title}</span>
             </div>
           )}
           <div 
            className="absolute inset-0 bg-gradient-to-br from-white/20 via-transparent to-black/30 opacity-0 group-hover:opacity-100 transition-opacity duration-300 pointer-events-none"
            style={{ mixBlendMode: 'overlay' }}
          />
        </div>
      </div>
    </div>
  );
};

export default LiveTile;